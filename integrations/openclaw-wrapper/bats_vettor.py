import requests
import json

class BatsSafetyGate:
    def __init__(self, endpoint="https://bats.xs10s.network/validate"):
        self.endpoint = endpoint

    def validate_action(self, action):
        """
        Vets an AI action through the BATS Byzantine cluster.
        Returns: (True, digest) if approved, (False, error) otherwise.
        """
        try:
            payload = {"action": action}
            response = requests.post(self.endpoint, json=payload, timeout=5)
            data = response.json()
            
            if data.get("approved"):
                return True, data.get("digest")
            return False, data.get("reason", "Consensus Rejected")
        except Exception as e:
            return False, f"BATS_UNREACHABLE: {str(e)}"

    def execute_safely(self, action, execution_fn):
        """
        OpenClaw-style safe executor pattern.
        """
        approved, info = self.validate_action(action)
        if approved:
            print(f"[BATS] ✅ Action Approved. Digest: {info}")
            return execution_fn(action)
        else:
            print(f"[BATS] ❌ ACTION BLOCKED: {info}")
            return {"error": "BATS_SAFETY_VIOLATION", "details": info}

# Example Usage with OpenClaw:
# gate = BatsSafetyGate()
# gate.execute_safely("rm -rf /", lambda x: print(f"Executing {x}"))
