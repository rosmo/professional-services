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

Now you can test the policy evaluation:

```sh
cd cmd/caevaluator
go run . -http-requests ../../requests.yaml ../../policy.json
```

(you can also add `-json` flag for JSON output)

## Limitations

- Uses open source Mod Security Core Rule Set instead of Google-curated rules
- ASN and region code are determined from a list (not from Google sources), both
  GeoLite2 and GeoIP2 databases can be used
- Cannot determine whether reCAPTCHA would be triggered or not
- `urlDecode` and `urlDecodeUni` behave the same
- `utf8Unicode` lowercases the string
- `cve-canary` and `json-sqli-canary` are not included as part of CRS 3.3.2
  - Examples of the rules are included (but not guaranteed to match Cloud Armor's)
- `SRC_IPS_V1` are not supported (migrate to `ipInRange`)
- `opt_in_rule_ids` are not supported
- `preconfiguredWafConfig` is not supported
- `evaluateAdaptiveProtection` and `evaluateAdaptiveProtectionAutoDeploy()` will always evaluate to false
- Rate limiting is not supported