# IAP helper for Git

The `git-iap` helper is a binary which retrieves an OIDC token for a service account. It then
configures that token as an extra header for Git through the `http.extraHeader` mechanism. This
allows standard Git tools to access HTTPS based repositories through 
[Identity-Aware Proxy](https://cloud.google.com/iap).

The binary should be installed in `$PATH` before the real `git` (it will look for the real `git` binary 
in the search path).

## Building and installing

You can build the binary on Go 1.16+:

```sh
# go install github.com/GoogleCloudPlatform/professional-services/tools/git-iap/cmd/git-iap
```

To install the binary in the path, you can do something like this (on Linux):

```sh
# mkdir /usr/local/bin/git-iap
# mv $GOPATH/bin/git-iap /usr/local/bin/git-iap/git
# export PATH=/usr/local/bin/git-iap/:$PATH
```

## Setting up a service account

The tool will use impersonation to assume the identity of the configured service account
(see below). When you create a service account, grant the service account 
`roles/iap.httpsResourceAccessor` IAM role (IAP-secured Web App User) and then the individual
users access to the service account using `roles/iam.serviceAccountTokenCreator` (Service Account 
Token Creator).

## Initial configuration

The `git-iap` helper stores the credentials in user configuration directory (on Linux: `$HOME/.config/git-iap`, 
on Windows: `%LocalAppData%\git-iap`, on OS X: `$HOME/Application Support/git-iap`) in an INI file.

To set up the initial configuration, you should run the helper with the appropriate flags:

```
git-iap -audience [IAP_AUDIENCE].apps.googleusercontent.com -service-account [SERVICE_ACCOUNT]@[PROJECT].iam.gserviceaccount.com -repository https://[YOUR-GIT-REPOSITORY]
```

Once the helper is set up and in `PATH`, it will retrieve a new token and store it in the configuration
directory in an obfuscated format (machine specific) and call the real `git` binary and pass the token
through the `extraHeader` setting.


