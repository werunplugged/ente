# Unplugged (UP) API

These are the endpoints the Unplugged fork adds to museum, or changes from upstream Ente. Upstream endpoints that are not listed here behave as in Ente.

Wiring:
- UP-only routes are registered in `pkg/up/up_server.go` (`RegisterRoutes`).
- Overridden upstream routes are re-pointed in `cmd/museum/main.go` at lines marked `// UP`.

| Method | Path | Auth | Handler |
|---|---|---|---|
| POST | `/users/up/ott` | UP access token | `UPUserHandler.SendOTT` |
| DELETE | `/up-admin/user/delete` | UP access token + admin role | `UPAdminHandler.UPDeleteUser` |
| GET | `/up-admin/users` | UP access token + admin role | `UPAdminHandler.UPGetUsers` |
| POST | `/webhooks/up/subscriptions` | HMAC signature | `UPWebhookHandler.HandleSubscriptionWebhook` |
| POST | `/users/verify-email` | none (overridden) | `UPUserHandler.VerifyEmail` |
| GET | `/users/srp/attributes` | none (overridden) | `UPUserHandler.GetSRPAttributes` |
| POST | `/users/change-email` | Ente token (overridden) | `UPUserHandler.ChangeEmail` |

## Identity model

Each UP user has a UP username, which is the Keycloak `preferred_username`. Their museum account email is always:

```
<username>@<unplugged.email-host>
```

That mapping is implemented by `utils.UPEmail` and `utils.UPUsernameFromEmail` (in `pkg/utils/up_user.go`).

- A username must not be empty and must not contain `@` or whitespace.
- `utils.User.GetUserID(username)` looks up the account by the hash of that email.
- It never matches any other email.

## Authentication

### UP access token (`UPAccessTokenMiddleware`)

Clients send `Authorization: Bearer <Keycloak access token>`. The token is an RS256 JWT, validated against these config keys:

| Key | Meaning |
|---|---|
| `jwt.public-keys` | RSA public keys (PEM). `jwt.public-key` is accepted as a single-key fallback. |
| `jwt.issuers` | Allowed `iss` values. |
| `jwt.audiences` | Allowed `aud` values. |

`exp` is required. `preferred_username` must be present and must be a valid UP username.

After the token is validated, the middleware sets these request headers for handlers. Any client-supplied values are removed first.

| Header | Value |
|---|---|
| `X-UP-Username` | the token's `preferred_username` (bare username, not the email) |
| `X-Auth-User-ID` | museum user ID for that username; **absent** when no account exists yet (sign-up) |

A validated token is cached for `min(time until exp, 60s)`.

| Status | Cause |
|---|---|
| 401 | missing header, invalid token, missing or invalid `preferred_username` |
| 500 | account lookup failed (DB error) |

### Admin role (`UPAdminRoleMiddleware`)

`/up-admin/*` additionally requires one of the token's `realm_access.roles` to be listed in `jwt.admin-roles`.
- If `jwt.admin-roles` is empty, every request is rejected and a warning is logged at startup.
- The admin does not need a museum account.

| Status | Cause |
|---|---|
| 403 | no matching role |

### Webhook signature (`WebhookSignatureMiddleware`)

The request must carry this header:

```
X-Webhook-Signature: base64( HMAC-SHA256( raw request body, unplugged.webhook-secret ) )
```

The comparison is constant-time.

| Status | Cause |
|---|---|
| 401 | header missing or signature wrong |
| 503 | `unplugged.webhook-secret` is not configured (fails closed) |

## Endpoints

### POST `/users/up/ott`

Signs a UP user in, or signs them up, using only the access token. No emailed OTT is needed. On success the response is a museum session, the same as upstream `POST /users/verify-email` returns.

Middleware: UP access token, then the per-user API rate limit.

Request:

```json
{ "purpose": "signup", "mobile": false }
```

| Field | Notes |
|---|---|
| `purpose` | `"signup"` or `"login"`. Any other value → 400. |
| `mobile` | Passed through to the OTT logic. |
| `email`, `client` | Ignored. The identity comes from the token. |

Flow:
1. `purpose == "signup"` and the account already exists → **409** `USER_ALREADY_REGISTERED`.
2. If an earlier sign-up stopped before key attributes were set (`USER_SIGNUP_INCOMPLETE`), that account and its billing are deleted so the sign-up can start over.
3. Any pending OTT for the email is removed.
4. The user is logged in. An account is created on the fly if none exists, for both purposes, as upstream `onVerificationSuccess` does.
5. The subscription is synced from the UP inner API (see [Subscription sync](#subscription-sync)).

Response 200 (`ente.EmailAuthorizationResponse`):

```json
{
  "id": 123,
  "keyAttributes": { "...": "..." },
  "encryptedToken": "...",
  "token": "...",
  "passkeySessionID": "",
  "twoFactorSessionID": "",
  "twoFactorSessionIDV2": "",
  "accountsUrl": "..."
}
```

Errors:

| Status | Cause |
|---|---|
| 400 | invalid purpose or username |
| 404 | no UP subscription found for the username. The account is already created or logged in by this point. |
| 409 | `USER_ALREADY_REGISTERED`, or the subscription is already claimed by another account |
| 500 | other failures |

### DELETE `/up-admin/user/delete?username=<username>`

Deletes the museum account of a UP user.

`username` is the bare UP username, not an email.

Steps:
1. Remove emergency contacts.
2. Release the billing transaction ID (`UPHandleAccountDeletion`). Accounts on the free plan need no billing action.
3. Delete the account (upstream `HandleAccountDeletion`).
4. Send a Discord admin notification.
5. Audit it as `user-deleted`.

Response 200: the upstream account-deletion result.

| Status | Cause |
|---|---|
| 400 | `username` missing or invalid; subscription not cancelled; unexpected product ID (family or empty) |
| 401 / 403 | auth or role failure |
| 404 | no account for the username |

### GET `/up-admin/users?sinceTime=<µs>`

Lists accounts created after `sinceTime` (Unix microseconds, required) and before now, oldest first. Accounts without an email are skipped.

Response 200:

```json
{ "users": [ { "ID": 123, "email": "alice@msg.example", "name": "", "hash": "...", "creationTime": 1700000000000000,
                "familyAdminID": null, "isTwoFactorEnabled": null, "isEmailMFAEnabled": null } ] }
```

`ID` is capitalized because `ente.User.ID` has no JSON tag. The response includes **decrypted emails**.

| Status | Cause |
|---|---|
| 500 | `sinceTime` missing or not an integer. The parse error is not mapped to 400. |

### POST `/webhooks/up/subscriptions`

The UP backend calls this endpoint when a user's subscription changes. It is a public route: rate-limited and signature-checked.

Request (`ente.WebhookRequest`):

```json
{
  "id": "sub_...",
  "type": "PREMIUM",
  "interval": "YEAR",
  "price": 49.99,
  "currency": "USD",
  "provider": "STRIPE",
  "nextChargeDate": 1735689600000,
  "expirationDate": 1735689600000,
  "serverTimestamp": 1704067200000,
  "username": "alice",
  "event": "subscription.updated"
}
```

Only `username` is used. The other fields, including `event`, are logged or ignored. Every call triggers a full [subscription sync](#subscription-sync) for that user, so the body carries no trusted subscription state.

Response 200: `{}`.

| Status | Cause |
|---|---|
| 400 | invalid username |
| 401 / 503 | signature problem (see above) |
| 404 | no account, or no UP subscription |
| 409 | the transaction is claimed by another account |

### Overridden upstream endpoints

| Endpoint | Difference from upstream |
|---|---|
| `POST /users/verify-email` | `email` in the body is the **bare username**. The server appends `@<unplugged.email-host>` before verifying the OTT. |
| `GET /users/srp/attributes?email=` | `email` may be a UP username. If it resolves to an account, that account's email is used. Otherwise the value is passed through unchanged, so plain emails still work. |
| `POST /users/change-email` (Ente token) | The OTT is accepted for either the bare username or `<username>@<email-host>`. **Known issue:** the account email is saved as the bare username, and `GetUserID` cannot resolve such accounts. Tracked on PR #58. |

## Subscription sync

`UPBillingController.UPVerifySubscription(userID)` is called by `/users/up/ott` and by the webhook.

1. It gets the UP subscription:
   - `GET {unplugged.inner-api-host}/inner/v2/by-username?username=<username>`, with a 10 s timeout and no auth header.
   - It uses the first element of the returned array. An empty array → 404.
2. `type == "BASIC"`: the user is set to the museum free plan, unless they are already on it.
3. Any other type is mapped to an `unplugged` subscription:

   | Subscription field | Source |
   |---|---|
   | `ProductID`, `Price` | `priceId` |
   | `OriginalTransactionID` | `subscriptionId` |
   | `ExpiryTime` | `expirationDate` |
   | `Period` | `interval` |
   | `Storage` | `PremiumPlanStorage` |
   | `IsCancelled` | `endReason == "CANCELED"` |

   The record is not replaced when any of these hold:
   - It is an outdated purchase: same product with an earlier expiry. This is a no-op.
   - It would be a downgrade below current usage → 400.
   - The transaction already belongs to another user → 409.

## Configuration

These keys are under `unplugged:` and `jwt:`. See `configurations/example.yaml`.

| Key | Default | Used by |
|---|---|---|
| `unplugged.api-host` | `https://up-app-dev.unpluggedsystems.app` | |
| `unplugged.inner-api-host` | `https://up-app-dev.unpluggedsystems.app` | subscription sync |
| `unplugged.email-host` | `msg.unpluggedsystems.app` | identity mapping |
| `unplugged.basic-plan-id` | `free` | free-plan detection |
| `unplugged.webhook-secret` | — (required for the webhook) | webhook signature |
| `jwt.public-keys` | — | UP access token |
| `jwt.issuers` | — | UP access token |
| `jwt.audiences` | — | UP access token |
| `jwt.admin-roles` | — (empty → `/up-admin` disabled) | admin role |

## Audit

`UPSecurityAudit` (`pkg/middleware/up_security_audit.go`) emits audit events for selected routes:
- `POST /users/verify-email` (login);
- `DELETE /up-admin/user/delete` (`user-deleted`);
- file, collection and share routes.

Identity fields come only from the headers that the auth middleware sets.