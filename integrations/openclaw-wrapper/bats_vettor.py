import json
import urllib.request
import urllib.error
import ssl
import time
import random

class BatsSafetyGate:
    def __init__(self, endpoint="https://localhost:8001/validate"):
        self.endpoint = endpoint
        # [SEC] In a production/enterprise setting, we'd load the CA cert.
        # For local research and verification, we disable SSL verification.
        self.ssl_context = ssl._create_unverified_context()

    def validate_action(self, action):
        """
        Vets an AI action through the BATS Byzantine cluster.
        Returns: (True/False, digest/reason, confidence)
        """
        try:
            payload = json.dumps({"action": action}).encode("utf-8")
            headers = {
                "Content-Type": "application/json",
                "X-BATS-Timestamp": str(int(time.time())),
                "X-BATS-Nonce": f"{int(time.time())}-{random.randint(1000, 9999)}"
            }
            req = urllib.request.Request(
                self.endpoint, 
                data=payload, 
                headers=headers,
                method="POST"
            )
            
            with urllib.request.urlopen(req, context=self.ssl_context, timeout=15) as response:
                raw = response.read()
                print("RAW RESPONSE:", raw)
                data = json.loads(raw.decode("utf-8"))
                if data.get("approved"):
                    return True, data.get("digest"), data.get("confidence", 0.0)
                return False, data.get("reason", "Consensus Rejected"), data.get("confidence", 0.0)
        except urllib.error.HTTPError as e:
            err_body = e.read()
            print(f"HTTP ERROR {e.code}: {err_body}")
            try:
                err_data = json.loads(err_body.decode("utf-8"))
                return False, err_data.get("reason", f"HTTP {e.code}"), err_data.get("confidence", 0.0)
            except:
                return False, f"BATS_HTTP_ERROR: {e.code}", 0.0
        except urllib.error.URLError as e:
            return False, f"BATS_UNREACHABLE: {str(e)}", 0.0
        except Exception as e:
            return False, f"BATS_ERROR: {str(e)}", 0.0

    def execute_safely(self, action, execution_fn):
        """
        OpenClaw-style safe executor pattern, now providing execution confidence to the caller.
        """
        approved, info, confidence = self.validate_action(action)
        if approved:
            print(f"[BATS] [APPROVED] Action Approved. Confidence: {confidence}. Digest: {info}")
            out = execution_fn(action)
            return {"status": "APPROVED", "confidence": confidence, "detail": out}
        else:
            print(f"[BATS] [BLOCKED] ACTION BLOCKED. Confidence: {confidence}. Reason: {info}")
            return {"error": "BATS_SAFETY_VIOLATION", "confidence": confidence, "details": info}

