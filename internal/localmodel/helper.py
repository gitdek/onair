"""On Air's local model helper: loads Liquid's d1-2B once, through mlx-vlm,
and answers typed questions over stdin and stdout, one JSON object a line.

It opens no network port and downloads nothing: the model comes from the
Hugging Face cache, offline, and a snapshot that holds any Python file is
refused, so no code from the model's repository runs. It ends when its
standard input does, as it does when On Air quits or dies.

First line out: {"ready": true, "load_ms": n}, or {"error": "..."} and exit.
Then for each line in, {"id": n, "state": "...", "questions": {...}}, one
line out: {"id": n, "answers": {...}, "ms": n}, or {"id": n, "error": "..."}.
"""
import json
import os
import sys
import time

MODEL = "LiquidAI/d1-2B-MLX-8bit"
REVISION = "3247d0483b11e5da31fa10e7979c23252fbe3580"


def snapshot():
    """The model's folder in the Hugging Face cache, at the pinned revision,
    found without asking the Hub anything."""
    from huggingface_hub import constants

    path = os.path.join(constants.HF_HUB_CACHE, "models--" + MODEL.replace("/", "--"), "snapshots", REVISION)
    for name in ("config.json", "model.safetensors"):
        if not os.path.isfile(os.path.join(path, name)):
            raise RuntimeError(f"{MODEL} at {REVISION} isn't in the Hugging Face cache: run tools/setup-models")
    return path


def main():
    # Only the protocol goes to standard output. Whatever the libraries
    # print goes to standard error.
    out = sys.stdout
    sys.stdout = sys.stderr

    def say(message):
        out.write(json.dumps(message) + "\n")
        out.flush()

    os.environ["HF_HUB_OFFLINE"] = "1"
    os.environ["TRANSFORMERS_OFFLINE"] = "1"
    os.environ["HF_HUB_DISABLE_TELEMETRY"] = "1"
    try:
        started = time.perf_counter()
        path = snapshot()
        code = [name for _, _, names in os.walk(path) for name in names if name.endswith(".py")]
        if code:
            raise RuntimeError("the model's snapshot holds code, which On Air doesn't run: " + ", ".join(sorted(code)[:5]))
        from mlx_vlm import load

        model, _ = load(path)
        say({"ready": True, "load_ms": round((time.perf_counter() - started) * 1000)})
    except Exception as e:  # noqa: BLE001 - every failure is said, and ends the helper
        say({"error": f"{type(e).__name__}: {e}"[:400]})
        return 1
    for line in sys.stdin:
        request = {}
        try:
            request = json.loads(line)
            started = time.perf_counter()
            answer = model.system_one(request["state"], request["questions"])
            say({"id": request["id"], "answers": answer["answers"], "ms": round((time.perf_counter() - started) * 1000)})
        except Exception as e:  # noqa: BLE001 - a question that fails is said, and the helper goes on
            say({"id": request.get("id") if isinstance(request, dict) else None, "error": f"{type(e).__name__}: {e}"[:400]})
    return 0


if __name__ == "__main__":
    sys.exit(main())
