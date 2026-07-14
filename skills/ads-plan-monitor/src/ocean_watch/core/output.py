import json
from pathlib import Path


def render_json(value):
    return json.dumps(value, ensure_ascii=False, indent=2)


def write_json(value, destination=None, output_fn=print):
    rendered = render_json(value)
    if destination:
        path = Path(destination)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(rendered + "\n", encoding="utf-8")
    output_fn(rendered)
