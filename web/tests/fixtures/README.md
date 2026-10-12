# Loopback TLS test fixture

`localhost-test-key.pem` and its self-signed certificate belong only to the disposable
loopback servers in `capacity-client.test.mjs`. The key is intentionally public, carries no
deployment identity and must never be used by an operator. BrrpBrrp generates installation
certificates independently. Secret-scanner exceptions apply to the historical fixture's
exact fingerprint, not to future private keys.
