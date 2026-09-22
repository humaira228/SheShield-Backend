# sheshield-backend

Minimal Go REST API replacing Firebase (Auth + Firestore) for the SheShield
app. Standard library `net/http` (Go 1.22+ method/path routing), sqlite
(pure-Go driver, no cgo) for storage, JWT for sessions.

## Run it

```bash
cp .env.example .env      # Windows PowerShell: copy .env.example .env
# open .env and set JWT_SECRET to any long random string (32+ characters)
go mod tidy               # first time only: downloads dependencies, creates go.sum
go run ./cmd/api
```

`.env` is read automatically on startup. Real environment variables take
priority over it, so production can set secrets the normal way instead.

Server listens on `:8080` by default. The DB file is created automatically at
`./data/sheshield.db` on first run -- no manual migration step needed.
Check it's up: open http://localhost:8080/health -- it should say `ok`.

## Endpoints

| Method | Path                  | Auth | Body                                                              |
|--------|-----------------------|------|--------------------------------------------------------------------|
| POST   | `/api/v1/auth/signup` | no   | `{name,email,password,phone,countryCode,gender,userType}` — `userType` is `user`\|`helper`\|`user_helper`; only `gender:"female"` may pick `user`/`user_helper`, everyone else is forced to `helper` (enforced server-side) |
| POST   | `/api/v1/auth/login`  | no   | `{email,password}`                                                 |
| GET    | `/api/v1/auth/me`     | yes  | —                                                                  |
| GET    | `/api/v1/contacts`    | yes  | —                                                                  |
| POST   | `/api/v1/contacts`    | yes  | `{name,relation?,phone,countryCode}` — phone is digits only (6-15), countryCode like `+880`; max 10 per user, no duplicate numbers |
| DELETE | `/api/v1/contacts/{id}` | yes | —                                                                  |
| POST   | `/api/v1/alerts`      | yes  | `{latitude?,longitude?,accuracyMeters?,notifiedByDevice?}` — `notifiedByDevice` = contact ids the phone already texted from its own SIM; the server texts the rest. Returns per-contact `deliveries` with `status` `sent`/`simulated`/`failed` |
| PATCH  | `/api/v1/auth/me`     | yes  | `{name?,phone?,countryCode?,address?}` — only these fields; email, gender, role and verified status cannot be changed here |
| POST   | `/api/v1/verification` | yes | multipart: `nidFront`, `nidBack`, `selfie` (JPEG/PNG, 5 MB each). Helper accounts only. Creates a *pending* request |
| GET    | `/api/v1/verification` | yes | `{status: none\|pending\|approved\|rejected, note?, submittedAt?}` |
| GET    | `/health`             | no   | —                                                                  |

Auth endpoints return `{"data": {"user": {...}, "token": "<jwt>"}}`.
Authenticated requests send `Authorization: Bearer <jwt>`.
Errors come back as `{"error": "message"}` with a 4xx/5xx status —
the Flutter side can show `error` directly, same as the old
`AuthFailure.message` pattern.

## Why these choices

- **sqlite over Postgres**: zero setup for a student/sprint project —
  swap `internal/db` for a `pgx` connection later without touching
  `auth`/`contact` packages; they only depend on `*sql.DB`.
- **JWT over sessions**: stateless, matches a mobile client that just
  stores a token securely (`flutter_secure_storage` on the Flutter side)
  and attaches it as a header — no server-side session store to run.
- **No framework**: Go 1.22's `net/http.ServeMux` now supports
  `"POST /path/{id}"` patterns directly, so a router library (chi/gin)
  wasn't needed for an API this size.

## Extending

Add a new feature the same way `contact/` is built: `model.go` →
`repository.go` → `service.go` (if there's real business logic beyond
CRUD) → `handler.go` with its own `Register(mux, jwtSecret)`, then one
line in `cmd/api/main.go`.

## Helper verification (admin)

A helper uploads the front and back of their ID and a selfie. The request is
**pending** until an admin decides. The app can never mark anyone verified:
the only code path that sets `is_helper_verified` is the admin command below.
There is deliberately no network endpoint for approving helpers.

Run these on the machine that has the database (the server may keep running):

```bash
go run ./cmd/admin pending                       # who is waiting
go run ./cmd/admin show <id>                     # details + where the photos are
go run ./cmd/admin approve <id>
go run ./cmd/admin reject <id> "ID photo is blurry"   # applicant sees the reason
```

What to check in `show`: the selfie matches the ID photo, the ID is genuine,
readable and not expired, and the name on it matches the account name.

Photos are stored in `UPLOAD_DIR` (default `./data/uploads`), one folder per
request, and are never served over HTTP. `data/` is git-ignored: never commit it.

## SOS texts: how they are sent

The app texts contacts from the phone's own SIM when it can (Android; works
with no internet). It then tells `POST /api/v1/alerts` which contacts that
covered, and the **server** texts the rest (iPhones, no SIM, permission denied,
send failed) through `internal/sms`.

`SMS_PROVIDER=log` (the default) sends nothing; alerts are stored and the
messages are printed. Such deliveries are reported as `simulated`, never
`sent`, so the app can be honest about it.

## Before going public

**ID documents are sensitive personal data. Before real users:**

- [ ] Encrypt uploaded documents at rest, or move them to private object storage
- [ ] Strip EXIF metadata from photos (it can contain GPS coordinates)
- [ ] Decide how long ID photos are kept (delete after the decision?) and enforce it
- [ ] Delete a user's uploaded documents when they delete their account
- [ ] Record *which admin* approved each request (audit trail)
- [ ] Privacy policy that says what you collect and why

Lead times first -- start these early:

- [ ] Choose a BTRC-listed SMS aggregator, register a sender ID (days to weeks)
- [ ] Implement `Sender` for it in `internal/sms`, add a case in `sms.New`
- [ ] Google Play: `SEND_SMS` is a restricted permission (needs an approved
      declaration), or ship a build without it that relies on server SMS
- [ ] Host the API publicly behind HTTPS (the app currently allows plain http
      in debug builds only)

Then:

- [ ] Rate-limit `/api/v1/alerts` and login (cost and abuse protection)
- [ ] Set a real `JWT_SECRET` and lock `CORS_ORIGIN` down
- [ ] Move from sqlite to a hosted database, with backups
- [ ] Privacy policy and consent flow (you store contacts' phone numbers and
      users' locations)
- [ ] Do not use `SMS_PROVIDER=log` in production (it logs numbers/locations)
