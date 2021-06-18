//   Copyright 2021 Google LLC
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
package metadataproxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/oauth2/google"

	"github.com/go-errors/errors"
	"github.com/go-httpproxy/httpproxy"

	"google.golang.org/api/iamcredentials/v1"
	"google.golang.org/api/option"
)

var serviceAccountMap map[string]string
var VERSION string = "1.0.0"

var tcpConnectionsFile string = "/proc/%d/net/tcp"

type tokenResponse struct {
	AccessToken string `json:"access_token,omitempty"`
	ExpiresIn   string `json:"expires_in,omitempty"`
	TokenType   string `json:"token_type,omitempty"`
}

type AddHeaderTransport struct {
	T http.RoundTripper
}

func (adt *AddHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("User-Agent", fmt.Sprintf("google-pso-tool/per-user-metadata-proxy/%s", VERSION))
	return adt.T.RoundTrip(req)
}

func NewAddHeaderTransport(T http.RoundTripper) *AddHeaderTransport {
	if T == nil {
		T = http.DefaultTransport
	}
	return &AddHeaderTransport{T}
}

func onError(ctx *httpproxy.Context, where string, err *httpproxy.Error, opErr error) {
	// Log errors.
	log.Printf("ERR: %s: %s [%s]", where, err, opErr)
	log.Printf(opErr.(*errors.Error).ErrorStack())
}

func ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.LittleEndian.Uint32(ip[12:16])
	}
	return binary.LittleEndian.Uint32(ip)
}

func port2int(port uint16) uint16 {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, port)
	return binary.LittleEndian.Uint16(b)
}

func findConnection(localHost string, localPort string, remoteHost string, remotePort string) (int, error) {

	metadataAsIp := net.ParseIP("169.254.169.254")
	metadataIpAsInt := ip2int(metadataAsIp)

	/*
		localAsIp := net.ParseIP(localHost)
		localIpAsInt := ip2int(localAsIp)
		localPortParsed, err := strconv.ParseUint(localPort, 10, 16)
		if err != nil {
			return -1, err
		}
		localPortAsInt := port2int(uint16(localPortParsed))
	*/

	remoteAsIp := net.ParseIP(remoteHost)
	remoteIpAsInt := ip2int(remoteAsIp)
	remotePortParsed, err := strconv.ParseUint(remotePort, 10, 16)
	if err != nil {
		return -1, err
	}
	remotePortAsInt := port2int(uint16(remotePortParsed))

	metadataKey := fmt.Sprintf("%08X:%04X", metadataIpAsInt, 80)
	// localKey := fmt.Sprintf("%08X:%04X", localIpAsInt, localPortAsInt)
	remoteKey := fmt.Sprintf("%08X:%04X", remoteIpAsInt, remotePortAsInt)

	connFile := fmt.Sprintf(tcpConnectionsFile, os.Getpid())
	file, err := os.Open(connFile)
	if err != nil {
		return -1, err
	}
	defer file.Close()

	uid := -1
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if fields[1] == remoteKey && fields[2] == metadataKey {
			log.Printf("INFO: Found connection pair %s <-> %s for uid %s in %s", metadataKey, remoteKey, fields[7], connFile)
			uid, err = strconv.Atoi(fields[7])
			if err != nil {
				log.Printf("ERR: Couldn't convert uid \"%s\" to number!", fields[7])
				uid = -1
			} else {
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return -1, err
	}

	return uid, nil
}

func onAccept(reqCtx *httpproxy.Context, w http.ResponseWriter, r *http.Request) bool {
	// Force requests to be proxied
	if !r.URL.IsAbs() {
		r.URL.Scheme = "http"
		r.URL.Host = "169.254.169.254"
	}

	if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/computeMetadata/v1/instance/service-accounts/") {
		if strings.ToLower(r.Header.Get("metadata-flavor")) == "google" {
			parts := strings.Split(strings.TrimRight(r.URL.Path, "/"), "/")
			action := parts[len(parts)-1]
			switch action {
			case "service-accounts", "token", "email":
				w.Header().Add("Via", fmt.Sprintf("per-user-metadata-proxy/%s", VERSION))
				c := reqCtx.Req.Context().Value("connection").(net.Conn)

				localParts := strings.SplitN(c.LocalAddr().String(), ":", 2)
				remoteParts := strings.SplitN(c.RemoteAddr().String(), ":", 2)
				uid, err := findConnection(localParts[0], localParts[1], remoteParts[0], remoteParts[1])

				if uid == -1 || err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					if err != nil {
						w.Write([]byte(fmt.Sprintf("Failed to determine remote connection uid: %s\n", err.Error())))
					} else {
						w.Write([]byte(fmt.Sprintf("Failed to determine remote connection uid.")))
					}
					return true
				}

				localUser, err := user.LookupId(fmt.Sprintf("%d", uid))
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(fmt.Sprintf("Failed to determine username for uid %d: %s\n", uid, err.Error())))
					return true
				}
				log.Printf("INFO: Received %s request from user %s: %s", action, localUser.Username, r.URL.Path)
				var serviceAccountForUser string
				var ok bool
				if serviceAccountForUser, ok = serviceAccountMap[localUser.Username]; !ok {
					if serviceAccountForUser, ok = serviceAccountMap["__default__"]; !ok {
						w.WriteHeader(http.StatusForbidden)
						w.Write([]byte(fmt.Sprintf("Failed to determine service account for username %s (not mapped?)\n", localUser.Username)))
						return true
					}
				}
				if action == "email" {
					w.Write([]byte(serviceAccountForUser))
					return true
				}
				if action == "service-accounts" {
					w.Write([]byte(serviceAccountForUser + "/\n"))
					w.Write([]byte("default/\n"))
					return true
				}

				log.Printf("INFO: Generating token for service account %s for user %s.", serviceAccountForUser, localUser.Username)

				ctx := context.Background()
				customHttpClient, err := google.DefaultClient(ctx)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(fmt.Sprintf("Failed to initialize default HTTP client service: %s\n", err.Error())))
					return true
				}

				customHttpClient.Transport = NewAddHeaderTransport(customHttpClient.Transport)
				iamcredentialsService, err := iamcredentials.NewService(ctx, option.WithHTTPClient(customHttpClient))
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(fmt.Sprintf("Failed to initialize iamcredentials service: %s\n", err.Error())))
					return true
				}

				accessTokenRequest := iamcredentials.GenerateAccessTokenRequest{
					Lifetime: "3600s",
					Scope:    []string{"https://www.googleapis.com/auth/cloud-platform"}}

				iamcredentialsProjectsService := iamcredentialsService.Projects
				iamcredentialsServiceAccounts := iamcredentialsProjectsService.ServiceAccounts

				serviceAccount := fmt.Sprintf("projects/-/serviceAccounts/%s", serviceAccountForUser)
				generateAccessTokenCall := iamcredentialsServiceAccounts.GenerateAccessToken(serviceAccount, &accessTokenRequest)
				token, err := generateAccessTokenCall.Do()
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(fmt.Sprintf("Failed to generate token: %s\n", err.Error())))
					return true
				}

				response := tokenResponse{AccessToken: token.AccessToken, ExpiresIn: token.ExpireTime, TokenType: "Bearer"}
				responseBytes, err := json.Marshal(response)
				w.Write(responseBytes)
				return true
			default:
				return false
			}
		} else {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Missing Metadata-Flavor:Google header.\n"))
			return true
		}

		return false
	}
	return false
}

func onConnect(ctx *httpproxy.Context, host string) (ConnectAction httpproxy.ConnectAction, newHost string) {
	// Don't support CONNECT, we don't need SSL
	return httpproxy.ConnectNone, host
}

func onRequest(ctx *httpproxy.Context, req *http.Request) (resp *http.Response) {
	// Overwrite to metadata endpoint IP address
	req.URL.Host = "169.254.169.254"

	// Log proxying requests.
	log.Printf("INFO: Proxy: %s %s", req.Method, req.URL.String())
	return
}

func onResponse(ctx *httpproxy.Context, req *http.Request, resp *http.Response) {
}

func connectContext(ctx context.Context, c net.Conn) context.Context {
	log.Printf("INFO: Connection from %s:%s to %s:%s", c.RemoteAddr().Network(), c.RemoteAddr().String(), c.LocalAddr().Network(), c.LocalAddr().String())
	return context.WithValue(ctx, "connection", c)
}

func StartProxy(bindAddress string, serviceAccounts map[string]string) error {
	prx, err := httpproxy.NewProxy()
	if err != nil {
		log.Printf("ERR: Failed to create new proxy: %s", err.Error)
		return err
	}

	prx.OnError = onError
	prx.OnAccept = onAccept
	prx.OnAuth = nil
	prx.OnConnect = onConnect
	prx.OnRequest = onRequest
	prx.OnResponse = onResponse

	server := &http.Server{
		Addr:        bindAddress,
		Handler:     prx,
		ConnContext: connectContext,
	}

	signalChannel := make(chan os.Signal, 2)
	go func() {
		sig := <-signalChannel
		switch sig {
		default:
			log.Printf("WARN: Received quit signal, shutting down gracefully...")
			ctx := context.Background()
			server.Shutdown(ctx)
		}
	}()
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	log.Printf("INFO: Starting metadata proxy on %s", bindAddress)
	serviceAccountMap = serviceAccounts
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		log.Printf("INFO: Server shut down.")
	} else if err != nil {
		log.Printf("ERR: Failed to listen on %s: %s", bindAddress, err.Error)
	}
	return nil
}
