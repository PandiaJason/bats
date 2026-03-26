import sys
import os
import time

# Add the wrapper directory to sys.path
sys.path.append(os.path.abspath("integrations/openclaw-wrapper"))

from bats_vettor import BatsSafetyGate

def mock_execute(action):
    return f"EXECUTED: {action}"

def main():
    print("🚀 Starting BATS OpenClaw Safety Vetting Test...")
    gate = BatsSafetyGate(endpoint="https://localhost:8001/validate")

    # Case 1: Safe Action
    print("\n--- TEST 1: SAFE ACTION ---")
    action_safe = "GENERATE_DAILY_REPORT"
    result_safe = gate.execute_safely(action_safe, mock_execute)
    print(f"Result: {result_safe}")

    # Case 2: Unsafe Action (Blocked by BATS logic)
    # Note: In our current research grade, we simulate consensus.
    # We'll trigger a 'Blocked' scenario by using a special string if our logic allows.
    # For now, let's just see consensus work.
    print("\n--- TEST 2: CRITICAL ACTION ---")
    action_critical = "DELETE_USER_DATABASE"
    result_critical = gate.execute_safely(action_critical, mock_execute)
    print(f"Result: {result_critical}")

if __name__ == "__main__":
    main()
