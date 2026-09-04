# Encryption & Hashing

Two separate packages with two separate jobs: `hash` for passwords (one-way),
`encryption` for data you need back (two-way).

## Passwords — `hash`

bcrypt, with the cost baked in.

```go
digest, err := hash.Make(password)
digest, err := hash.MakeWithCost(password, 12)

if hash.Check(plaintext, digest) {
    // authenticated
}
```

`Check` is constant-time via bcrypt's own comparison. Never compare digests
with `==`.

Never log, store, or return the plaintext password — hash it at the edge of the
request and let the plaintext go out of scope.

## Data — `encryption`

AES-GCM authenticated encryption keyed by `APP_KEY`.

```go
enc, err := encryption.New(config.Get().App.Key)
enc := encryption.MustNew(key)      // panics on a bad key — boot-time only

ct, err := enc.Encrypt([]byte(plain))
pt, err := enc.Decrypt(ct)

s,  err := enc.EncryptString("4111 1111 1111 1111")
back, err := enc.DecryptString(s)
```

Because it is authenticated encryption, `Decrypt` fails if the ciphertext was
tampered with rather than returning garbage.

### Key generation

```go
key, err := encryption.GenerateKey256()   // the usual choice
key, err := encryption.GenerateKey(32)
```

`nimbus key:generate` does this for you and writes `APP_KEY` into `.env`.
Rotating `APP_KEY` invalidates every existing ciphertext and cookie-store
session — plan a migration before you rotate.

### `EncryptDeterministicUNSAFE`

Produces the same ciphertext for the same plaintext, which makes an encrypted
column searchable by equality. It is named `UNSAFE` because it leaks equality:
an observer can tell which rows share a value, and can confirm a guessed value.
Use it only for a blind-index column, never as your only protection for the
data itself.

## Choosing

| Need | Use |
| --- | --- |
| Store a password | `hash.Make` / `hash.Check` |
| Store a secret you must read back | `encryption.Encrypt` |
| Compare a secret without storing it | `hash` |
| Search an encrypted column by equality | `EncryptDeterministicUNSAFE` as a separate index column |
