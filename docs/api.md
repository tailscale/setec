# setec API

> WARNING: This API is still under active development, and subject to change.
> This document is up-to-date as of 15-Jan-2026.

The setec service exports an API over HTTPS. All methods of the API are called
via HTTPS POST, with request and response payloads transmitted as JSON.

Calls are authenticated via Tailscale and authorized using peer capabilities.
The peer capability label is `tailscale.com/cap/secrets`.

Calls to the API must include a header `Sec-X-Tailscale-No-Browsers: setec`.
This prevents browser scripts from initiating calls to the service.


## HTTP Status

- Invalid request parameters report 400 Invalid request.
- Access permission errors report 403 Forbidden.
- Requests for unknown values report 404 Not found.
- All other errors report 500 Internal server error.


## Permissions

The service defines named _actions_ that are subject to access control:

- `get`: Denotes permission to fetch the contents of a secret.  Note that `get`
  does not imply `info`, or vice versa.

- `info`: Denotes permission to read the metadata for a secret, including
  available and active version numbers, but not the secret values.

- `put`: Denotes permission to put a new value of a secret.

- `create-version`: Denotes permission to create a specific version of a
   secret, but not overwrite an existing version.

- `activate`: Denotes permission to set one one of of the available versions of
  a secret as the active one.

- `delete`: Denotes permission to delete secret versions, either individually
  or entirely.


## Methods

- `/api/list`: List metadata for all secrets to which the caller has `info`
  permission.

  **Request:** `api.ListRequest` (empty, send `null` or `{}`).

  **Response:** array of `api.SecretInfo`

  **Example response:**
  ```json
  [{"Name":"example","Versions":[1,2,3],"ActiveVersion":2}]
  ```

- `/api/get`: Get the value for a single secret.

  **Requires:** `get` permission for the specified secret.

  **Request:** `api.GetRequest`

  **Example requests:**
  ```json
  {"Name":"example"}                -- fetch the active version
  {"Name":"example","Version":0}    -- fetch the active version
  {"Name":"example","Version":15}   -- fetch the specified version

  {"Name":"example","Version":2,"UpdateIfChanged":true}  -- see below
  ```

  **Response:** `api.SecretValue`

  **Example responses:**
  ```json
  {"Value":"aGVsbG8sIHdvcmxk","Version":15}
  ```

  **Conditional get:** If a request includes a `"Version"` and sets
  `"UpdateIfChanged": true` the server returns the latest active version of the
  secret if and only if the latest active version number is different from
  `"Version"`. If the latest active version is equal to `"Version"` the server
  reports 304 Not modified and returns no value. This may be used to poll for
  updates without generating auditable access.

  If `"Version"` is unset or 0, the `"UpdateIfChanged"` flag is ignored and the
  latest active version is returned unconditionally.


- `/api/info`: Get metadata for a single secret.

  **Requires:** `info` permission for the specified secret.

  **Request:** `api.InfoRequest`

  **Example request:**
  ```json
  {"Name":"example"}
  ```

  **Response:** `api.SecretInfo`

  **Example response:**
  ```json
  {"Name":"example","Versions":[1,2,3],"ActiveVersion":2}
  ```

- `/api/put`: Add a new value for a secret.

  **Requires:** `put` permission for the specified name.

  **Request:** `api.PutRequest`

  **Example request:**
  ```json
  {"Name":"example","Value":"YSBuZXcgYmVnaW5uaW5n"}
  ```

  **Response:** `api.SecretVersion`

  **Example response:**
  ```json
  4
  ```

  If the value added is exactly equal to the existing active version of the
  secret, the server reports the existing active version without modifying the
  store.

- `/api/create-version`: Creates a new version of a secret, sets its value and
  immediately activates that version. It fails if the specified version number
  has already been used for this secret (even if deleted).  The specified
  version number must be > 0.

  **Requires:** `create-version` permission for the specified name.

  **Request:** `api.CreateVersionRequest`

  **Example request:**
  ```json
  {"Name":"example","Version": 2025, "Value":"YSBuZXcgYmVnaW5uaW5n"}
  ```

  **Response:** `null`

- `/api/activate`: Set the active version of an existing secret.

  **Requires:** `activate` permission for the specified name.

  **Request:** `api.ActivateRequest`

  **Example request:**
  ```json
  {"Name":"example","Version":4}
  ```

  **Response:** `null`

- `/api/delete`: Delete all versions of the specified secret.

  **Requires:** `delete` permission for the specified name.

  **Request:** `api.DeleteRequest`

  **Example request:**
  ```json
  {"Name":"example"}
  ```

  **Response:** `null`

- `/api/delete-version`: Delete a single non-active version of a secret.

  **Requires:** `delete` permission for the specified name.

  **Request:** `api.DeleteVersionRequest`

  **Example request:**
  ```json
  {"Name":"example","Version":2}
  ```

  **Response:** `null`
