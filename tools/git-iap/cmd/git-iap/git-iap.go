package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/gob"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	gitiap "github.com/GoogleCloudPlatform/professional-services/tools/git-iap"
	"github.com/denisbrodbeck/machineid"
	"golang.org/x/oauth2"
	ini "gopkg.in/ini.v1"
)

var nonceLength int = 12

func getObfuscationKey() []byte {
	id, err := machineid.ID()
	if err != nil || id == "" {
		id, err = os.Hostname()
		if err != nil {
			log.Fatal(err)
		}
	}
	hash := sha256.Sum256([]byte(id))
	return hash[:]
}

func obfuscateBuffer(buf []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(getObfuscationKey())
	if err != nil {
		return nil, nil, err
	}

	nonce := make([]byte, nonceLength)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	ciphertext := aesgcm.Seal(nil, nonce, buf, nil)
	return nonce, ciphertext, nil
}

func saveToken(tokenFile string, token *oauth2.Token) error {
	tf, err := os.OpenFile(tokenFile, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer tf.Close()

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	encoder.Encode(token)

	nonce, obfuscatedBuf, err := obfuscateBuffer(buf.Bytes())
	if err != nil {
		return err
	}
	tf.Write(nonce)
	tf.Write(obfuscatedBuf)
	return nil
}

func deobfuscateBuffer(nonce []byte, buf []byte) ([]byte, error) {
	block, err := aes.NewCipher(getObfuscationKey())
	if err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := aesgcm.Open(nil, nonce, buf, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func loadToken(tokenFile string) (*oauth2.Token, error) {
	tf, err := os.OpenFile(tokenFile, os.O_RDONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer tf.Close()

	nonce := make([]byte, nonceLength)
	_, err = io.ReadFull(tf, nonce)
	if err != nil {
		return nil, err
	}

	obfuscatedBuf, err := io.ReadAll(tf)
	buf, err := deobfuscateBuffer(nonce, obfuscatedBuf)
	if err != nil {
		return nil, err
	}

	reader := bytes.NewReader(buf)
	decoder := gob.NewDecoder(reader)

	var token oauth2.Token
	err = decoder.Decode(&token)
	if err != nil {
		return nil, err
	}

	return &token, nil
}

func main() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}

	configPath := filepath.Join(configDir, "git-iap")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Printf("Configuration directory doesn't exist, creating: %s", configPath)
		err = os.MkdirAll(configPath, 0700)
		if err != nil {
			panic(err)
		}
	}

	var repository string
	var audience string
	var sa string
	var gitBin string
	var token *oauth2.Token
	tokenFile := filepath.Join(configPath, "git-iap.token")
	configFile := filepath.Join(configPath, "git-iap.ini")
	osArgs := os.Args[1:]
	if _, err := os.Stat(configFile); !os.IsNotExist(err) {
		f, err := ini.InsensitiveLoad(configFile)
		if err != nil {
			log.Fatalf("Error reading configuration file %s: %s", configFile, err)
		}
		sec := f.Section("")
		_repository, err := sec.GetKey("repository")
		if err != nil {
			log.Fatalf("Error reading configuration file %s: missing repository (%s)", configFile, err)
		}

		_audience, err := sec.GetKey("audience")
		if err != nil {
			log.Fatalf("Error reading configuration file %s: missing audience (%s)", configFile, err)
		}

		_sa, err := sec.GetKey("service_account_email")
		if err != nil {
			log.Fatalf("Error reading configuration file %s: missing service account email (%s)", configFile, err)
		}
		repository = _repository.String()
		audience = _audience.String()
		sa = _sa.String()

		_gitBin, err := sec.GetKey("git")
		if err == nil {
			gitBin = _gitBin.String()
		}
	} else {
		log.Printf("Configuration file does not exist: %s", configFile)
		flag.StringVar(&repository, "repository", "", "Repository hostname")
		flag.StringVar(&gitBin, "git", "", "Git binary location")
		flag.StringVar(&audience, "audience", "", "OAuth client ID (audience)")
		flag.StringVar(&sa, "service-account", "", "Service account email (for impersonation)")
		flag.Parse()

		if repository == "" || audience == "" || sa == "" {
			log.Fatalf("Specify repository, audience and service account email!")
		}

		f := ini.Empty()
		sec := f.Section("")
		if gitBin != "" {
			sec.NewKey("git", gitBin)
		}
		sec.NewKey("repository", repository)
		sec.NewKey("audience", audience)
		sec.NewKey("service_account_email", sa)
		err := f.SaveTo(configFile)
		if err != nil {
			log.Fatalf("Error saving configuration file %s: %s", configFile, err)
		}
		err = os.Chmod(configFile, 0600)
		if err != nil {
			log.Fatalf("Failed to set file permissions for %s: %s", configFile, err)
		}
		log.Printf("Configuration file created: %s", configFile)
		osArgs = flag.Args()
		if len(osArgs) == 0 {
			os.Exit(0)
		}
	}

	gi := gitiap.NewGitIAP(audience, sa)

	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		token, err = loadToken(tokenFile)
		if err != nil {
			log.Fatalf("Failed to load token from file %s: %s", tokenFile, err)
		}
	}

	token, err = gi.GetIAPToken(token)
	err = saveToken(tokenFile, token)
	if err != nil {
		log.Fatalf("Failed to save token in file %s: %s", tokenFile, err)
	}

	originalPath := os.Getenv("PATH")
	newPath := originalPath
	if gitBin == "" {
		currentBin, err := os.Executable()
		if err != nil {
			log.Fatalf("Failed to get current executable: %s", err)
		}
		exPath := filepath.Dir(currentBin)
		spl := filepath.SplitList(originalPath)
		newPathList := make([]string, 0)
		for _, elem := range spl {
			if elem != exPath {
				newPathList = append(newPathList, elem)
			}
		}
		newPath = strings.Join(newPathList, string(os.PathListSeparator))
		os.Setenv("PATH", newPath)
		gitBin, err = exec.LookPath("git")
		if err != nil {
			log.Fatalf("Failed to find 'git': %s", err)
		}
	}
	os.Setenv("PATH", originalPath)

	var gitArgs []string = []string{gitBin, "-c", fmt.Sprintf("http.%s.extraheader=Proxy-Authorization: Bearer %s", repository, token.AccessToken)}
	gitArgs = append(gitArgs, osArgs...)
	err = syscall.Exec(gitBin, gitArgs, os.Environ())
	if err != nil {
		log.Fatalf("Failed to execute 'git': %s", err)
	}
}
