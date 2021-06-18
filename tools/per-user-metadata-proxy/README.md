# ![Per user metadata proxy](img/logo-small.png)

`per-user-metadata-proxy` is a proxy server that can provide separate Service Account identities for workloads running
under different users on a single Compute Instance server. It uses the `/proc` filesystem's list of TCP connections
to find the local identity of the workload and maps any gcloud/Cloud SDK/curl requests to another service account.

The server runs by default on port `12972` on `localhost` and relies on iptables or nftables to intercept non-`root`
user's access to the internal metadata endpoint (`metadata.google.internal` or `169.254.169.254`). It passes through
all other requests while intercepting token-generating ones. 

> :warning: **SECURITY**: This tool has not received a security audit, which means there might be ways to bypass it.

## Building

* Requires Golang 1.16+.

You can build it by running:

```sh
go get github.com/GoogleCloudPlatform/professional-services/tools/per-user-metadata-proxy
```

## Service account setup

You will need to grant `roles/iam.serviceAccountTokenCreator` to the target service accounts for the 
Compute Engine instance's service account.

Example (instance is running with `instance@project.iam.gserviceaccount.com`):

```sh
gcloud iam service-accounts create target-account
gcloud iam service-accounts add-iam-policy-binding target-account@project.iam.gserviceaccount.com --member="serviceAccount:instance@project.iam.gserviceaccount.com" --role="roles/iam.serviceAccountTokenCreator"
```

## Running

```sh
per-user-metadata-proxy localuser=service-account@project.iam.gserviceaccount.com anotheruser=other-service-account@project.iam.gserviceaccount.com
```

You can also map an username called `__default__` to provide a fallback service account for non-mapped users.

A `systemd` unit file has been provided as well, which can be installed as follows:

```sh
echo 'CMDLINE="localuser=service-account@project.iam.gserviceaccount.com anotheruser=other-service-account@project.iam.gserviceaccount.com"' | (umask 007; cat > /etc/per-user-metadata-proxy)
cp per-user-metadata-proxy.service /etc/systemd/system
systemctl daemon-reload
systemctl enable per-user-metadata-proxy
systemctl start per-user-metadata-proxy
```
(please adapt the binary location in the unit file according to where you have installed it in)

## Sample iptables configuration

```sh
iptables -t nat -I OUTPUT -p tcp -d 169.254.169.254/32 --dport 80 -m owner ! --uid-owner 0 -j DNAT --to-destination 127.0.0.1:12972

# Block other ports: port 8080 is used for hypervisor ATLS handshaker service address, and 8081 for snapshots service
iptables -I OUTPUT -p tcp -d 169.254.169.254/32 -m multiport ! --dports 53,80,8081 -m owner ! --uid-owner 0 -j REJECT
```

## Sample nftables configuration

```sh
nft add table nat
nft 'add chain nat output { type nat hook output priority 100 ; }'
nft add rule nat output meta skuid != 0 ip daddr 169.254.169.254/32 tcp dport 80 dnat to 127.0.0.1:12972 

# Block other ports: port 8080 is used for hypervisor ATLS handshaker service address, and 8081 for snapshots service
nft add rule filter OUTPUT meta skuid != 0 ip daddr 169.254.169.254/32 tcp dport != '{53,80,8081}' reject
```

### Example of the proxy in operation

```sh
[normaluser@centos8 ~]$ sudo curl -H 'Metadata-Flavor: Google' http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/email
instance@project.iam.gserviceaccount.com

[normaluser@centos8 ~]$ curl -H 'Metadata-Flavor: Google' http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/email
target-account@project.iam.gserviceaccount.com
```