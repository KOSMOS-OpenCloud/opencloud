## OpenCloud Wrapper

A tool that wraps the OpenCloud binary and provides a way to re-configure the running OpenCloud instance.

When run, **ocwrapper** starts an API server that exposes some endpoints to re-configure the OpenCloud server.

### Usage

1.  Build

    ```bash
    make build
    ```

2.  Run

    ```bash
    ./bin/ocwrapper serve --bin=<path-to-opencloud-binary>
    ```

    To check other available options:

    ```bash
    ./bin/ocwrapper serve --help
    ```

    ```bash
     --url string              OpenCloud server url (default "https://localhost:9200")
     --retry string            Number of retries to start OpenCloud server (default "5")
     -p, --port string         Wrapper API server port (default "5200")
     --admin-username string   admin username for OpenCloud server
     --admin-password string   admin password for OpenCloud server
    ```

Access the API server at `http://localhost:5200`.

Also, see `./bin/ocwrapper help` for more information.

### API

**ocwrapper** exposes two endpoints:

1.  `PUT /config`

    Updates the configuration of the running OpenCloud instance.
    Body of the request should be a JSON object with the following structure:

    ```json
    {
      "ENV_KEY1": "value1",
      "ENV_KEY2": "value2"
    }
    ```

    Returns:

    - `200 OK` - OpenCloud is successfully reconfigured
    - `400 Bad Request` - request body is not a valid JSON object
    - `500 Internal Server Error` - OpenCloud server is not running

2.  `DELETE /rollback`

    Rolls back the configuration to the starting point.

    Returns:

    - `200 OK` - rollback is successful
    - `500 Internal Server Error` - OpenCloud server is not running

3.  `POST /command`

    Executes the provided command on the OpenCloud server. The body of the request should be a JSON object with the following structure:

    ```yml
    {
      "command": "<opencloud-command>", # without the OpenCloud binary. e.g. "list"
    }
    ```

    If the command requires user input, the body of the request should be a JSON object with the following structure:

    ```json
    {
      "command": "<opencloud-command>",
      "inputs": ["value1"]
    }
    ```

    Returns:

    ```json
    {
      "status": "OK",
      "exitCode": 0,
      "message": "<command output>"
    }
    OR
    {
      "status": "ERROR",
      "exitCode": <error-exit-code>,
      "message": "<command output>"
    }
    ```

    - `200 OK` - command is successfully executed
    - `400 Bad Request` - request body is not a valid JSON object
    - `500 Internal Server Error`

4.  `POST /start`

    Starts the OpenCloud server.

    Returns:

    - `200 OK` - OpenCloud server is started
    - `409 Conflict` - OpenCloud server is already running
    - `500 Internal Server Error` - Unable to start OpenCloud server

5.  `POST /stop`

    Stops the OpenCloud server.

    Returns:

    - `200 OK` - OpenCloud server is stopped
    - `500 Internal Server Error` - Unable to stop OpenCloud server
