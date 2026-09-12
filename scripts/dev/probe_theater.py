"""Assert the Theater-compatible HTTP flow; --ci uses an isolated temporary server.
Live mode requires FAKEMBY_PROBE_USER / FAKEMBY_PROBE_PASSWORD; no default credentials.
The live probe logs in and logs out but does not import or update playback progress.
"""
import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

def run(base, username, password, admin_key=None):
    opener = urllib.request.build_opener(NoRedirect)
    def request(method, path, token=None, data=None, expected=200, admin=False):
        headers = {"Content-Type": "application/json"}
        if token: headers["X-Emby-Token"] = token
        if admin: headers["X-Api-Key"] = admin_key
        body = json.dumps(data).encode() if data is not None else None
        req = urllib.request.Request(base + path, data=body, headers=headers, method=method)
        try:
            response = opener.open(req, timeout=10)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read()
            if response.code != expected:
                raise AssertionError(f"{method} {path.split('?')[0]}: expected {expected}, got {response.code}")
            print(f"PASS {method} {path.split('?')[0]} [{response.code}]")
            return json.loads(raw) if raw and expected not in (302, 204) else None
    if admin_key:
        users = request("GET", "/api/admin/users", admin=True)
        uid = next(u["id"] for u in users if u["name"] == "admin")
        request("POST", f"/api/admin/users/{uid}/password", data={"password": password}, expected=204, admin=True)
        result = request("POST", "/api/admin/import", data={"library":"Probe movies", "items":[{"Id":"probe-movie","name":"Probe movie","type":"Movie","sources":[{"name":"Probe source","url":"https://example.invalid/probe.mp4","container":"mp4"}]}]}, admin=True)
        if result["errors"] or result["imported"] != 1: raise AssertionError("isolated fixture import failed")
    login = request("POST", "/emby/Users/AuthenticateByName", data={"Username":username,"Pw":password})
    token, uid = login["AccessToken"], login["User"]["Id"]
    try:
        me = request("GET", "/emby/Users/Me", token)
        for key in ("LatestItemsExcludes", "OrderedViews", "MyMediaExcludes", "GroupedFolders"):
            if not isinstance(me["Configuration"].get(key), list): raise AssertionError(f"Configuration.{key} must be array")
        for path in ("/emby/System/Endpoint", "/emby/System/Configuration", "/emby/Items/Filters", "/emby/Channels", "/emby/Shows/NextUp?UserId="+uid, "/emby/Genres", "/emby/Studios"):
            request("GET",path,token)
        views = request("GET",f"/emby/Users/{uid}/Views",token)["Items"]
        for library in views:
            request("GET",f"/emby/Users/{uid}/Items/{library['Id']}",token)
        items=request("GET",f"/emby/Users/{uid}/Items?Recursive=true&IncludeItemTypes=Movie,Episode&Limit=10",token)["Items"]
        for item in items:
            detail=request("GET",f"/emby/Users/{uid}/Items/{item['Id']}",token)
            for key in ("Genres","People","MediaSources","ImageTags","ProviderIds"):
                if detail.get(key) is None: raise AssertionError(f"{key} must be non-null")
            if not detail["MediaSources"]: continue
            info=request("POST",f"/emby/Items/{item['Id']}/PlaybackInfo",token,{})
            for source in info["MediaSources"]:
                if not isinstance(source.get("RequiredHttpHeaders"),dict): raise AssertionError("Missing RequiredHttpHeaders")
                if token in source["DirectStreamUrl"]: raise AssertionError("Token leaked in playback URL")
                request("GET",source["DirectStreamUrl"],token,expected=302)
            if admin_key:
                body={"ItemId":item["Id"],"PlaySessionId":info["PlaySessionId"],"PositionTicks":10000000}
                request("POST","/emby/Sessions/Playing/Progress",token,body,204)
                request("POST","/emby/Sessions/Playing/Stopped",token,body,204)
    finally:
        request("POST","/emby/Sessions/Logout",token,expected=204)

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ci",action="store_true")
    args=parser.parse_args()
    if not args.ci:
        user=os.environ.get("FAKEMBY_PROBE_USER")
        password=os.environ.get("FAKEMBY_PROBE_PASSWORD")
        if not user or not password: parser.error("Set FAKEMBY_PROBE_USER and FAKEMBY_PROBE_PASSWORD, or use --ci")
        run(os.environ.get("FAKEMBY_PROBE_BASE","http://127.0.0.1:8096"),user,password)
        return
    with tempfile.TemporaryDirectory(prefix="fakemby-probe-") as temp:
        binary=Path(temp)/("fakemby.exe" if os.name=="nt" else "fakemby")
        subprocess.run(["go","build","-o",str(binary),"./cmd/fakemby"],cwd=ROOT,check=True)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1",0));port=sock.getsockname()[1]
        key,password=secrets.token_hex(24),secrets.token_hex(18)
        env=os.environ.copy()
        env.update({"CONFIG_FILE":str(Path(temp)/"absent.yaml"),"FAKEMBY_SERVER_HOST":"127.0.0.1","FAKEMBY_SERVER_PORT":str(port),"FAKEMBY_DATABASE_DIALECT":"sqlite","FAKEMBY_DATABASE_PATH":str(Path(temp)/"probe.db"),"FAKEMBY_IMAGE_CACHE_DIR":str(Path(temp)/"cache"),"FAKEMBY_ADMIN_API_KEY":key,"FAKEMBY_PLAYBACK_SIGN_KEY":secrets.token_hex(32),"FAKEMBY_LOG_FILE":"","FAKEMBY_LOG_LEVEL":"error"})
        with open(Path(temp)/"server.log","wb") as log:
            process=subprocess.Popen([str(binary)],cwd=temp,env=env,stdout=log,stderr=log)
            try:
                base=f"http://127.0.0.1:{port}"
                for _ in range(100):
                    if process.poll() is not None: raise RuntimeError("isolated server exited before readiness")
                    try:
                        with urllib.request.urlopen(base+"/readyz",timeout=.5): break
                    except (OSError,urllib.error.URLError): time.sleep(.1)
                else: raise RuntimeError("isolated server readiness timeout")
                run(base,"admin",password,key)
            finally:
                process.terminate()
                try: process.wait(timeout=10)
                except subprocess.TimeoutExpired: process.kill();process.wait()
    print("Theater HTTP assertions passed; this is not a native-client UI test.")
if __name__=="__main__":main()
