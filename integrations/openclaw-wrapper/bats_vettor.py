import json
import urllib.request
import urllib.error
import ssl

class BatsSafetyGate:
    def __init__(self, endpoint="https://localhost:8001/validate"):
        self.endpoint = endpoint
        # [SEC] In a production/enterprise setting, we'd load the CA cert.
        # For local research and verification, we disable SSL verification.
        self.ssl_context = ssl._create_unverified_context()

    def validate_action(self, action):
        """
        Vets an AI action through the BATS Byzantine cluster.
        Returns: (True, digest) if approved, (False, error) otherwise.
        """
        try:
            payload = json.dumps({"action": action}).encode("utf-8")
            req = urllib.request.Request(
                self.endpoint, 
                data=payload, 
                headers={"Content-Type": "application/json"},
                method="POST"
            )
            
            with urllib.request.urlopen(req, context=self.ssl_context, timeout=15) as response:
                raw = response.read()
                print("RAW RESPONSE:", raw)
                data = json.loads(raw.decode("utf-8"))
                if data.get("approved"):
                    return True, data.get("digest")
                return False, data.get("reason", "Consensus Rejected")
        except urllib.error.HTTPError as e:
            err_body = e.read()
            print(f"HTTP ERROR {e.code}: {err_body}")
            return False, f"BATS_HTTP_ERROR: {e.code} {err_body}"
        except urllib.error.URLError as e:
            return False, f"BATS_UNREACHABLE: {str(e)}"
        except Exception as e:
            return False, f"BATS_ERROR: {str(e)}"

    def execute_safely(self, action, execution_fn):
        """
        OpenClaw-style safe executor pattern.
        """
        approved, info = self.validate_action(action)
        if approved:
            print(f"[BATS] [APPROVED] Action Approved. Digest: {info}")
            return execution_fn(action)
        else:
            print(f"[BATS] [BLOCKED] ACTION BLOCKED: {info}")
            return {"error": "BATS_SAFETY_VIOLATION", "details": info}
