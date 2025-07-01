# Install the OWASP Core Rule Set

Installing the OWASP Core Rule Set in this directory:

```sh
curl -L https://github.com/coreruleset/coreruleset/archive/refs/tags/v3.3.2.tar.gz | tar -zx --strip-components 1 -
```

The OWASP CRS contains some rules with Perl syntax that are unsupported by [Coraza](https://github.com/corazawaf/coraza/).
For more discussion, see: [Failed to compile CRS rules v3.3.4 with coraza v2](https://github.com/corazawaf/coraza/issues/509)

To find the rules:

```sh
# grep '(\?\!)' crs/rules/*.conf
# grep '++' crs/rules/*.conf
```