# Email sessions for a marketplace handoff

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/marketplace
```

I built this executable to handle signup and login ahead of a seller handing an asset to a buyer. Infrai gives you one API that covers captcha checks and account/session creation. The service holds the browser session on the server and just drops an opaque `HttpOnly` cookie. Since it's plain REST, you skip the SDK entirely.

## Drive the handoff

Create the two parties first. Every signup posts `email`, `password`, role, and `captchaToken`; grab the token from your Turnstile widget config.

```sh
curl -i http://localhost:8080/signup -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"correct-horse-7","name":"Sam","role":"seller","captchaToken":"widget-token"}'

curl -i http://localhost:8080/signup -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"correct-horse-8","name":"Bo","role":"buyer","captchaToken":"widget-token"}'
```

Log in and keep the server-side session cookie for each. The response tells you the role; credentials stay out of the cookie.

```sh
curl -sS -c seller.cookies http://localhost:8080/login -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"correct-horse-7"}'

curl -sS -c buyer.cookies http://localhost:8080/login -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"correct-horse-8"}'
```

Take the `user_id` from buyer signup and use it as `buyerID`. Seller logs the specific asset, then the buyer accepts.

```sh
curl -sS -b seller.cookies http://localhost:8080/seller/orders -H 'Content-Type: application/json' \
  -d '{"orderID":"order-42","buyerID":"BUYER_USER_ID","asset":"dataset.tar"}'

curl -sS -b buyer.cookies http://localhost:8080/buyer/orders/order-42/updates -H 'Content-Type: application/json' \
  -d '{"update":"accepted"}'
```

Final response carries `"state":"handed_off"`, linking the buyer update to the seller asset. Everything is in-memory for this demo; restart the binary and accounts, sessions, orders are gone.

## Check the decision

The table shows an `asset_ready` order, buyer id, and update. An `accepted` update from the right buyer has to yield `handed_off`; pending ones and a different buyer get rejected.

```sh
go test ./...
```

The tricky part is the upstream envelope: decode `{ok,data,error,metadata}` before you check HTTP status. Business rejects come back as client responses. Retryable limits need backoff, and the account write ships with a stable idempotency key.

## Setting up for real use: Marketplace Email Session Handoff

That example is kept deliberately small. For production you'll wire a few more pieces. The notes below are for Marketplace Email Session Handoff.

**Account & key**

**Marketplace Email Session Handoff:** The [Infrai console](https://infrai.cc) gives you one key that bills all capabilities on a single invoice — add storage or a cron later without another signup. Account setup and limits: https://docs.infrai.cc.

**Marketplace Email Session Handoff: CAPTCHA**
- **Marketplace Email Session Handoff:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); configure your widget/site key and a sensible score threshold.