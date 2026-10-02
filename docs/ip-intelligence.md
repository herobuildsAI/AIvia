# Configure IP intelligence

Open **Settings → IP intelligence**. Saving configuration does not send a lookup. To use it, select a service, enable **IP intelligence** in Diagnostics, and click **Run network checks**. Each check can query at most eight distinct observed public IPs; equivalent address spellings share a lookup. There is no background refresh or automatic retry.

## Built-in ipapi.is

Select **ipapi.is** and enter your own API key. The endpoint is fixed to `https://api.ipapi.is/`. A saved key takes precedence over `AIVPN_IPAPI_KEY` from the application environment. Without either key, intelligence remains unavailable; missing evidence is never treated as a clean IP. Account access, quota, charges, and data terms are managed by the provider. See [ipapi.is documentation](https://ipapi.is/developers.html).

Leave the key field blank to retain the current saved key at the same normalized provider and endpoint. Check **Remove the saved API key** to remove it from the active store. The environment fallback remains active until you unset it and restart the app. A prior store backup may retain the old key.

## Self-hosted compatible endpoint

Select **Self-hosted / compatible endpoint** and enter the full lookup URL. Supported destinations are public HTTPS on port 443, or a literal loopback HTTP(S) address such as `http://127.0.0.1:8080/lookup`. Private LAN destinations, URL credentials, queries, fragments, and redirects are rejected. DNS answers are checked and connections pinned; a public hostname cannot redirect the request into a private network.

The endpoint must accept a POST with `Content-Type: application/json`:

```json
{"q":"8.8.8.8","key":"your-key-or-empty-string"}
```

The key is optional for a compatible service. It is sent in the body, not in the URL. Arbitrary provider formats and bearer-header authentication are not supported. A server backed by an offline database can implement this same interface; this app does not directly load database files.

Example response (illustrative values only):

```json
{
  "ip":"8.8.8.8",
  "location":{"country_code":"US","state":"California","timezone":"America/Los_Angeles"},
  "asn":{"asn":15169},
  "is_abuser":false,
  "is_tor":false,
  "is_proxy":false,
  "is_vpn":false,
  "is_datacenter":true
}
```

The returned IP must match the requested public IP. Fields you do not know should be omitted or null; do not synthesize false flags. Errors, malformed responses, timeouts, and missing flags remain unknown. The UI identifies custom evidence as user-configured rather than attributing it to ipapi.is.

Custom lookups are declined when a configured proxy would prevent destination enforcement. The app does not silently use a direct route instead. Normal loopback bypass under standard proxy environment behavior remains supported.

## Key and consent handling

Keys saved in the UI are plaintext in `store.json` and potentially `store.json.bak`, with the store's existing user-directory protections. They are not returned by the API, prefilled into the browser, exported in reports/profiles, or sent to models. Use `AIVPN_IPAPI_KEY` if you prefer process configuration for the built-in provider.

Changing provider or endpoint does not carry a saved key to the new destination. Settings use revisions: stale saves are rejected, and a check cannot silently use a different provider configuration from the one the user saw. After saving or detecting a change, enable the optional lookup again. Configure only a service you trust with your public IP observations.
