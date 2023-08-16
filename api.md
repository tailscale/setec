# setec API

> WARNING: This API is still under active development, and subject to change.
> This document is up-to-date as of 16-Aug-2023.

The setec service exports an API over HTTP. All methods of the API are called
via HTTP POST, with request and response payloads transmitted as JSON.

Calls are authenticated via Tailscale and authorized using peer capabilities.
The peer capability label is `https://tailscale.com/cap/secrets`.

Calls to the API must include a header `Sec-X-Tailscale-No-Browsers: setec`.
This prevents browser scripts from initiating calls to the service.

## HTTP Status

- Invalid request parameters report 400 Invalid request.
- Access permission errors report 403 Forbidden.
- All other errors report 500 Internal server error.

## Methods

- `/api/list`: List metadata for all secrets to which the caller has `info`
  permission.

  **Request:** `api.ListRequest`

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
  {"Name":"example","Version":0}    -- fetch the active version
  {"Name":"example","Version":15}   -- fetch the specified version
  ```

  **Response:** `api.SecretValue`

  **Example responses:**
  ```json
  {"Value":"aGVsbG8sIHdvcmxk","Version":15}
  ```

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

- `/api/put`: Add a a new value for a secret.

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

- `/api/set-active`: Set the active version of an existing secret.

  **Requires:** `set-active` permission for the specified name.

  **Request:** `api.SetActiveRequest`

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
