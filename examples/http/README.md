# HTTP smoke test

This program verifies that `std/net/http` can issue a GET request, receive
response headers, and read the response body. Windows uses WinHTTP; Linux uses
the system libcurl shared library (`libcurl.so.4`).

Start the bundled Python HTTP test server in one terminal:

```sh
python server.py
```

Build and run on Windows:

```powershell
..\..\hikec.exe build -target windows main.hike -o http-smoke.exe
.\http-smoke.exe
```

Build and run on Linux:

```sh
../../hikec build -target linux main.hike -o http-smoke
./http-smoke
```

The test URL is intentionally fixed to localhost so the test does not depend
on an external service.
