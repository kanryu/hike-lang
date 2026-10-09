# HTTP smoke test

This program verifies that `std/net/http` can issue a GET request through
Windows WinHTTP, receive response headers, and read the response body.

Start the bundled Python HTTP test server in one terminal:

```powershell
python server.py
```

Then build and run in another terminal:

```powershell
..\..\hikec.exe build -target windows main.hike -o http-smoke.exe
.\http-smoke.exe
```

The test URL is intentionally fixed to localhost so the test does not depend
on an external service.
