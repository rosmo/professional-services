# Cloud Armor static evaluator

`ca-evaluator` is a tool to statically test Cloud Armor rules against predetermined
HTTP requests. It can help to ensure that a Cloud Armor policy will block all the 
bad things and allow all the good things.

The tool currently supports only L7 Cloud Armor policies, not L4.

## Usage

First, export your policy as a JSON (not YAML!) file:

```sh
# For global policies:
gcloud compute security-policies export global-policy --file-name=policy.yaml --file-format=json

# For regional policies: 
gcloud compute security-policies export regional-policy --file-name=policy.yaml --file-format=json --region europe-north1
```

Then, download the OWASP CRS rule sets: see [installation instructions](cmd/caevaluator/crs/README_INSTALL.md)

You can also install MaxMind Geolite database, see instructions [here](https://dev.maxmind.com/geoip/updating-databases/#directly-downloading-databases).

Now you can test the policy evaluation:

```sh
cd cmd/caevaluator
go run . -http-requests ../../requests.yaml -security-policy ../../policy.json
```

(you can also add `-json-output` flag for JSON output)

### Using with Terraform

You can evaluate Cloud Armor policies directly from Terraform via Hashicorp's
[`external`](https://registry.terraform.io/providers/hashicorp/external/latest/docs/data-sources/external)
data source.

Example:

```hcl
data "google_compute_security_policy" "policy" {
  name    = "test"
  project = "your-project-id"
}

data "external" "validator" {
  program = ["cmd/caevaluator/caevaluator", "-json-input", "-json-output", "-convert-terraform"]

  query = {
    requests        = jsonencode(yamldecode(file("requests.yaml")))
    security_policy = jsonencode(data.google_compute_security_policy.policy)
  }

  lifecycle {
    postcondition {
      condition     = self.result.success == "true"
      error_message = "Cloud Armor policy fails validation."
    }
  }
}
```

## Limitations

- Uses open source Mod Security Core Rule Set instead of Google-curated rules
- ASN and region code are determined from a list (not from Google sources), both
  GeoLite2 and GeoIP2 databases can be used. If not set or cannot be determined,
  ASN will be `0` and region code will be `US`
- Cannot determine whether reCAPTCHA would be triggered or not
- `urlDecode` and `urlDecodeUni` behave the same
- `utf8Unicode` lowercases the string
- `cve-canary` and `json-sqli-canary` are not included as part of CRS 3.3.2
  - Examples of the rules are included (but not guaranteed to match Cloud Armor's)
- `SRC_IPS_V1` are not supported (migrate to `ipInRange`)
- `preconfiguredWafConfig` is not supported
- `evaluateAdaptiveProtection` and `evaluateAdaptiveProtectionAutoDeploy()` will always evaluate to false, so they
  will never trigger a reject
- Rate limiting is not supported
- The tool does not automatically determine `origin.user_ip` from headers, specify it manually
  (if `user_ip` is not set, it will be set based on `ip` anyway)
