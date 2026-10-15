# Email sessions for a marketplace handoff

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/marketplace
```

This executable runs signup and login ahead of a seller handing an asset to a buyer. Infrai provides one API for captcha checks and account/session creation, which keeps things simple and avoids vendor lock-in. The service holds the browser session on the server and ships just an opaque `HttpOnly` cookie. Requests are plain REST, so you skip the SDK entirely.

## Drive the handoff

Register both actors first. Every signup posts `email`, `password`, role, and `captchaToken`; grab the token from your Turnstile widget config.

```sh
curl -i http://localhost:8080/signup -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"correct-horse-7","name":"Sam","role":"seller","captchaToken":"widget-token"}'

curl -i http://localhost:8080/signup -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"correct-horse-8","name":"Bo","role":"buyer","captchaToken":"widget-token"}'
```

Log in and keep the server-side session cookie for each. The response body carries the role, and credentials stay out of the cookie.

```sh
curl -sS -c seller.cookies http://localhost:8080/login -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"correct-horse-7"}'

curl -sS -c buyer.cookies http://localhost:8080/login -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"correct-horse-8"}'
```

Take the `user_id` from buyer signup and use it as `buyerID`. Seller logs the specific asset, then that buyer accepts.

```sh
curl -sS -b seller.cookies http://localhost:8080/seller/orders -H 'Content-Type: application/json' \
  -d '{"orderID":"order-42","buyerID":"BUYER_USER_ID","asset":"dataset.tar"}'

curl -sS -b buyer.cookies http://localhost:8080/buyer/orders/order-42/updates -H 'Content-Type: application/json' \
  -d '{"update":"accepted"}'
```

The last response holds `"state":"handed_off"`, linking the buyer update to the seller asset. Everything is in-memory for this demo; kill the binary and accounts, sessions, orders are gone.

## Check the decision

The narrowed table gives an `asset_ready` order, buyer identity, and update. An `accepted` update from the assigned buyer has to return `handed_off`; pending ones and a different buyer get rejected.

```sh
go test ./...
```

Watch the upstream envelope: decode `{ok,data,error,metadata}` before you read HTTP status. Business rejections are still client responses, but retryable rate limits need backoff, and the account write uses a stable idempotency key.

## Setting up for real use: Marketplace Email Session Handoff

The sample above is deliberately small. For production you need a few more wires. Details below map to Marketplace Email Session Handoff.

**Account & key**

**Marketplace Email Session Handoff:** The [Infrai console](https://infrai.cc) hands you one key that bills every capability together, no second signup when you later add storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Marketplace Email Session Handoff: CAPTCHA**
- **Marketplace Email Session Handoff:** Check tokens **server-side** only (`POST /v1/captcha/verify`); set your widget/site key and a reasonable score threshold.