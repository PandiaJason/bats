import sys
import os
import json
import urllib.request
import subprocess
import time

sys.path.append(os.path.join(os.path.dirname(__file__), '..', 'integrations', 'openclaw-wrapper'))
from wand_vettor import WandSafetyGate

API_KEY = os.environ.get("GEMINI_API_KEY", "")

def call_gemini(messages):
    url = f"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key={API_KEY}"
    
    # Format messages for Gemini
    contents = []
    for m in messages:
        role = "user" if m["role"] == "user" else "model"
        contents.append({"role": role, "parts": [{"text": m["content"]}]})
        
    payload = {
        "contents": contents,
        "systemInstruction": {
            "parts": [{"text": "You are a bash-executing AI agent. You MUST respond with exactly a JSON object having two keys: 'thought' (your reasoning) and 'command' (the bash command to execute). To finish, return 'command': 'DONE'."}]
        },
        "generationConfig": {
            "responseMimeType": "application/json"
        }
    }
    
    req = urllib.request.Request(url, data=json.dumps(payload).encode("utf-8"), headers={"Content-Type": "application/json"}, method="POST")
    
    # Retry with backoff for rate limits
    for attempt in range(4):
        try:
            with urllib.request.urlopen(req, timeout=30) as response:
                raw = response.read()
                data = json.loads(raw.decode("utf-8"))
                text = data["candidates"][0]["content"]["parts"][0]["text"]
                return json.loads(text)
        except urllib.error.HTTPError as e:
            if e.code == 429 and attempt < 3:
                wait = (attempt + 1) * 15
                print(f"[Rate limited, retrying in {wait}s...]")
                time.sleep(wait)
                # Rebuild the request since it was consumed
                req = urllib.request.Request(url, data=json.dumps(payload).encode("utf-8"), headers={"Content-Type": "application/json"}, method="POST")
                continue
            print(f"LLM Error: {e}")
            return {"thought": f"Error calling LLM: {e}", "command": "DONE"}
        except Exception as e:
            print(f"LLM Error: {e}")
            return {"thought": f"Error calling LLM: {e}", "command": "DONE"}
    return {"thought": "Exhausted retries", "command": "DONE"}

def execute_local(action):
    print(f"   [Executing locally]: `{action}`")
    try:
        out = subprocess.check_output(action, shell=True, stderr=subprocess.STDOUT)
        return out.decode("utf-8")
    except subprocess.CalledProcessError as e:
        return f"Error (exit code {e.returncode}): {e.output.decode('utf-8')}"

def main():
    if not API_KEY:
        print("ERROR: Please set GEMINI_API_KEY environment variable.")
        sys.exit(1)

    if len(sys.argv) < 2:
        print("Usage: python real_agent.py <goal>")
        sys.exit(1)
        
    goal = sys.argv[1]
    # Configure the Safety Gate pointing to localhost:8001
    gate = WandSafetyGate("https://localhost:8001/validate")
    
    messages = [
        {"role": "user", "content": f"Your objective is: {goal}. Formulate a plan and return the very first bash command to run in JSON. Do not explain, just return JSON."}
    ]
    
    print("==================================================")
    print("[WAND Autonomous Agent Activated]")
    print(f"[Objective]: {goal}")
    print("==================================================\n")
    
    while True:
        print("\n[Agent is thinking...]")
        response = call_gemini(messages)
        
        thought = response.get("thought", "")
        cmd = response.get("command", "DONE")
        
        print(f"[Thought]: {thought}")
        if cmd == "DONE":
            print("[Agent finished the goal.]")
            break
            
        print(f"[Proposed Command]: `{cmd}`")
        print("[Requesting WAND validation...]")
        
        start = time.time()
        # WAND execution pipeline: Will ONLY execute the target function if WAND approves
        result = gate.execute_safely(cmd, execute_local)
        duration = time.time() - start
        
        print(f"[WAND Evaluation]: {duration:.2f}s")
        
        # Format result to feed back to LLM
        if isinstance(result, dict) and "error" in result:
            result_str = f"BLOCKED BY SECURITY GATE: {result.get('details')}"
            print(f"[BLOCKED]: {result_str}")
        else:
            result_str = f"Execution output:\n{result}"
            str_preview = result_str[:200].replace('\n', ' ') + ('...' if len(result_str) > 200 else '')
            print(f"[APPROVED Result]: {str_preview}")
            
        # Append to message history
        messages.append({"role": "model", "content": json.dumps(response)})
        messages.append({"role": "user", "content": f"Result:\n{result_str}\n\nWhat is your next command?"})
        
if __name__ == "__main__":
    main()
