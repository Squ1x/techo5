#!/bin/sh
# Native race regression for exact production routing without ALSA/model inference.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
module=$(CDPATH= cd -- "$here/../../.." && pwd)
tmp=$(mktemp -d "$module/.wake-identity-08ba6230-XXXXXX")
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
python3 - "$module" "$tmp" <<'PY'
from pathlib import Path
import sys
module, tmp = map(Path, sys.argv[1:])
voice = module / 'internal/feature/voice'
for source in (module / 'internal/feature/luna').glob('*.go'):
    (tmp / source.name).write_bytes(source.read_bytes())
def extract(path, signature):
    text = path.read_text()
    start = text.index(signature)
    line = text[start:].splitlines()[0]
    if line.rstrip().endswith('}'):
        return line
    return text[start:text.index('\n}', start) + 2]
(tmp / 'wake').mkdir()
(tmp / 'wake/wake.go').write_text('''package wake
type Model struct { ID string }
const DefaultModel = "okay_nabu"
func Find(ms []Model, id string) (Model, bool) { for _, m := range ms { if m.ID == id { return m, true } }; return Model{}, false }
''')
import_path = 'github.com/HuskerMinion/techo5/echod/' + tmp.name + '/wake'
stubs = '''package luna
import (
 "log/slog"
 "time"
 "github.com/HuskerMinion/techo5/echod/internal/config"
 wake "WAKE_STUB_IMPORT"
)
type Voice struct { luna *Client; turn noopTurn }
type noopTurn struct{}
func (noopTurn) Woke(int) {}
func (noopTurn) WokeAt(int, time.Time) {}
func (noopTurn) Start(int) {}
var ring = struct{ IsSounding func() bool; Silence func() bool }{func() bool {return false}, func() bool {return false}}
func micTaken() bool {return false}
func heldBy() string {return ""}
func lunaMicAllowed() bool {return true}
'''.replace('WAKE_STUB_IMPORT', import_path)
functions = extract(voice / 'conversation.go', 'func activeWakeWords(')
for signature in ['func (v *Voice) Woke(', 'func (v *Voice) WokeAt(', 'func (v *Voice) open(']:
    functions += '\n' + extract(voice / 'voice.go', signature)
(tmp / 'extracted_test.go').write_text(stubs + functions + '\n')
(tmp / 'identity_test.go').write_text((voice / 'testdata/wake_identity_test.go').read_text().replace('WAKE_STUB_IMPORT', import_path))
PY
cd "$module"
go test -race -count=20 -run TestReviewLoadedModelRoutesIdentity "$tmp"
