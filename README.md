# simpus-antrian-interactive

Registration queue kiosk for patients, written with **MyGo native**
(`github.com/egoist/mygo`, Go). No webview and no web bundle: every screen is
drawn by MyGo from Go code.

This application only calls the APM facade `simpus-antrian-server`
(`/api/v1/apm/*`). Release 1 does not print tickets.

## Running

```powershell
cd touch
$env:GOPROXY="https://goproxy.io,direct"   # proxy.golang.org is unreachable on this machine
go tool mygo dev                            # kiosk window, hot reload
```

Other commands:

```powershell
go test ./...          # screen flow + configuration tests (no network)
go vet ./...
go tool mygo vet .     # MyGo-specific lifetime/goroutine check
go tool mygo build     # release to build/
```

## Configuration

`config.json` is looked up in this order:

1. `CONFIG_FILE`, if the environment variable is set. The file **must** exist;
   a wrong path is reported with the path spelled out.
2. Next to the executable — the normal case for a portable copy.
3. The working folder, so `go run` and `go tool mygo dev` also work.

Nothing about the first option changes the other two: a local `config.json`
beside the exe keeps working exactly as before.

```powershell
Copy-Item config.example.json config.json
```

| Field | Meaning |
|---|---|
| `server_url` | address of `simpus-antrian-server`, default `http://localhost:8080` |
| `api_key` | the server's `APM_API_KEY` value (header `x-api-key`) |
| `poscheck_id` | poscheck id; the server fetches the name of its location |
| `petugas_id` | id of the registration staff member recorded in `visits` |
| `mock` | `true` uses invented data in memory, without a server |
| `dry_run` | `true` = the server computes the number without writing to the database. The example file ships with `true` so a fresh install never writes; set it to `false` once the kiosk is ready to issue real queue numbers |
| `windowed` | `true` = a normal window that can be closed. Development only |
| `printer.name` | exact Windows printer name. Empty = printing off, which is the default |
| `printer.width` | characters per line: `32` for 58 mm paper, `48` for 80 mm. Default 32 |
| `printer.copies` | copies of each ticket. Default 1 |
| `autostart` | `true` = the kiosk adds itself to the Windows Run key so it opens after a reboot. Default `false` |

`config.example.json` ships with `dry_run: true`. This is deliberate: copying
the example, filling in `api_key` and `poscheck_id`, and starting the kiosk
will show real patients, real polyclinics, and real queue numbers **without
creating a visit**. Confirm the whole screen flow against the live server
first, then set `dry_run: false` so patients get a number that is recorded in
SIMPUS.

Leaving `dry_run: true` is safe but useless in production: patients receive a
number that the polyclinic never sees.

`config.json` and `log/` are not committed because they contain the API key.
There is no `.gitignore` in this repo — do not `git add` those files, and do
not add `config.json` to the repo.

When `mock: false`, `api_key` and `poscheck_id` must be filled in; the app
refuses to start with a message naming which field is empty. If the file was
not found at all, the message also says which path it looked in.

Logs go to `log/` **next to the executable**, never to the working directory,
so a shortcut or a scheduled task cannot scatter them somewhere else.

### Keeping config outside the app folder

Useful on more than one kiosk, and when the app folder must be replaceable
without losing the key. Once, as Administrator:

```powershell
New-Item -ItemType Directory -Force 'C:\ProgramData\simpus-antrian'
Copy-Item 'C:\simpus-antrian\config.example.json' 'C:\ProgramData\simpus-antrian\config.json'
notepad 'C:\ProgramData\simpus-antrian\config.json'

icacls 'C:\ProgramData\simpus-antrian' /inheritance:r `
  /grant:r 'BUILTIN\Administrators:(OI)(CI)F' 'BUILTIN\Users:(OI)(CI)RX'

[Environment]::SetEnvironmentVariable(
  'CONFIG_FILE', 'C:\ProgramData\simpus-antrian\config.json', 'Machine')
```

The variable only reaches **new** processes: log off and on, or reboot, before
trusting it. Open a fresh terminal and check `$env:CONFIG_FILE` before copying
a shortcut into Startup.

Without Administrator rights, the same thing per user:

```powershell
$dir = "$env:LOCALAPPDATA\simpus-antrian"
New-Item -ItemType Directory -Force $dir
Copy-Item 'C:\simpus-antrian\config.example.json' "$dir\config.json"
[Environment]::SetEnvironmentVariable('CONFIG_FILE', "$dir\config.json", 'User')
```

That kiosk must then run as the same Windows user that set the variable.

## Receipt printer

Printing is off until `printer.name` names a printer; an empty name is how it is
switched off, so there is no separate switch to fall out of step. When it is
set, every issued number prints one ESC/POS ticket through the Windows spooler:
no extra dependency, and the printer's own driver does the work.

Install the printer in Windows first, the way the vendor instructs, then copy
its name exactly — a typo is the most common failure:

```powershell
Get-Printer | Select-Object Name, DriverName
```

```json
"printer": { "name": "EPSON TM-T82 Receipt", "width": 32, "copies": 1 }
```

| Paper | `width` |
|---|---|
| 58 mm | 32 |
| 80 mm | 48 |

`copies: 2` prints the same ticket twice, for the patient and the file. The
ticket ends with a cut command, so the printer needs a cutter or staff tear the
paper.

Printing is **best effort**. The number is already on screen and in SIMPUS, so
a printer that is off, unplugged, or out of paper never blocks the patient and
never shows a message: the failure only goes to `log/` and the kiosk keeps
issuing numbers. That silence is why the Petugas menu has **Tes cetak**: it
sends a sample ticket and reports the result in the dialog, which is the only
place a broken printer becomes visible before a patient finds it.

The ticket is plain ASCII on purpose. Thermal printers boot into a single-byte
code page, not UTF-8, so anything outside ASCII prints as `?` rather than
arriving as garbage and shifting every column after it.

`dry_run: true` tickets print too, marked **MODE UJI**, so the printer can be
checked against the real server without creating a visit.

No printer on the dev PC? Any installed printer proves the plumbing, and
**Send To OneNote 16** is handy when it exists: its port is `nul:`, so the job
succeeds and nothing is printed.

```json
"printer": { "name": "Send To OneNote 16" }
```

## Autostart after a power cut

A kiosk that needs somebody to click an icon after an outage is not a kiosk.
Set `"autostart": true` and the app puts its own path in the Windows Run key
(`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value
`SimpusAntrianKiosk`). No Administrator rights, no service, no installer.

The entry is **reconciled on every start**, not written once:

| `autostart` | What the next start does |
|---|---|
| `true` | writes or repairs the entry so it points at the exe running now |
| `false` or missing | removes the entry |

That is what makes moving the app folder safe: unpack a new zip somewhere else,
start it once, and the old path is replaced. Turning autostart off is the same
one-line change.

Two guards:

- A binary running from the temp folder is **never** registered. `go run` and
  `go tool mygo dev` build into temp, and Windows would then try to start a file
  that no longer exists. The skip is written to `log/`.
- A registry that refuses to be written never stops the kiosk. It is an `ERROR`
  line in `log/`, and the kiosk opens as usual.

### Autostart is not auto-login

This is the part that surprises people. If the PC stops at the Windows login
screen after an outage, **nothing starts**, autostart or not: Windows runs the
Run key only after somebody logs in.

To bring the kiosk back with nobody present, the PC must log in by itself:

```powershell
netplwiz
```

Clear "Users must enter a user name and password", then enter the kiosk
account's password once. That stores the password in the machine's credential
store, so treat it as a physical-security decision for the site: anyone who
reaches the PC gets that session. If that is not acceptable, keep the kiosk
behind a locked door and accept that a power cut needs a person.

Check the entry without opening the app:

```powershell
Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name SimpusAntrianKiosk
```

## Screen flow

| Screen | Content | Action |
|---|---|---|
| 1. Start | two buttons: has identity / new patient | goes to screen 2 or straight to screen 4 (`mode=blank`) |
| 2. Identity | one box: RM number / NIK / BPJS number + numeric keyboard | `POST /apm/lookup` (`pcare=false`) |
| 3. Confirmation | name, RM no., address, BPJS status | "Bukan Saya" goes back, or continue |
| 4. Polyclinic | grid of active polyclinics | `GET /apm/poli` |
| 5. Doctor | today's doctors + practice hours + remaining quota | `GET /apm/dokter?poli_id=` |
| 6. Summary | all choices + BPJS status | `POST /apm/queue` |
| 7. Number | large queue code, polyclinic, doctor, remaining queue | "Selesai" returns to screen 1 |

Rules that bind the code:

- **The patient does not pick the identity type.** One box, three types; the
  length of the number decides (`ParseIdentifier` in `ident.go`):

  | Length | Read as | Sent as |
  |---|---|---|
  | 1–6 | RM number, left-padded with zeros | `no_rm` |
  | 13 | BPJS number | `no_bpjs` |
  | 16 | NIK | `nik` |
  | 7–12, 14, 15, 17+ | **not supported yet** | — (Cari button disabled) |

  The box accepts digits only: spaces, dots, and hyphens are stripped, so a
  number pasted from WhatsApp comes in clean. The box is an ordinary text
  input, so `Ctrl+V` (paste) and `Ctrl+C` (copy) work without extra buttons.
- Queue mode is never guessed: `ModeForLookup` (`api.go`) translates the
  server's lookup table. `existing_active` → `registration`,
  `existing_inactive`/`new` → `blank`, `existing_bpjs_unknown` → follows
  `can_register`.
- `mode=blank` does **not** send an identity or `petugas_id`; the server
  rejects it with `400 INVALID_INPUT`.
- `pcare=true` at registration so that "Nama Faskes" is filled in SIMPUS.
- `ALREADY_REGISTERED` (409) is not a failure: the number from
  `data.antrian.kode` is still shown with the note "sudah terdaftar".
- `QUEUE_BUSY` is retried after 1s, then 2s, before giving up.

## Kiosk mode

The app is locked down by default for mounting at the registration desk: full
screen, always on top, no close/minimize/maximize buttons, cannot be resized
or dragged, and does not appear in the taskbar. The window uses a normal
Windows frame (not frameless) so that full screen can be entered and left
without breaking position and size. The window is created with
`kioskOptions` (`kiosk.go`) from `mygo.WindowOptions`.

During development, set `"windowed": true` in `config.json` so the window can
be resized and closed like an ordinary app. The `kiosk_test.go` test makes
sure each lock cannot disappear by accident.

### Switching to a normal window without exiting

The Petugas dialog has one button whose label follows the current mode:
**"Pakai jendela biasa"** in full screen, **"Kembali ke layar penuh"** when
already in windowed mode. Useful when staff need to open another application
without shutting down the kiosk.

The normal window produced by this button is a standard Windows window: it has
a title bar, can be dragged, resized, minimized, and maximized. The X button
does **not** close the app (`OnClose` refuses); closing still goes through the
"Keluar" button in the same dialog, so the event is recorded in the log.

The mode is not persisted across restarts: every time it runs, the kiosk goes
back to full screen.

### Leaving the kiosk (staff)

The small **Petugas** button in the bottom-right corner of the footer opens the
**Menu Petugas**. If a keyboard is present, `Ctrl` + `Alt` + `Q` does the same
thing. Inside there is a display option, then a "Keluar" button with
confirmation.

The button is deliberately small and dimly coloured so patients do not take it
for part of the flow, yet it stays visible — the kiosk must always have an exit
that a person can find.

> History: the first version used a 5-tap gesture on an invisible 56×56 box in
> the corner. That failed in real testing — staff could not find it. When the
> box was enlarged to 140×140, it instead covered the "Ya, Ini Saya" button.
> A visible button is the right choice. Right-click / long-press were rejected
> too: on Windows touch screens a long press becomes a right-click, and nobody
> looks for a right-click on a kiosk with no context menu.

While the dialog is open, the 120-second timer stops — staff are not thrown
back to the start screen while working.

## Timer

Every screen except screen 1 returns to the start after **120 seconds** without
touch. A countdown warning appears in the last 20 seconds. The timer uses the
frame clock (`c.Now()` + `c.After`), not `time.Now()`, so tests can advance time
with `tt.Advance`.

## Structure

| File | Content |
|---|---|
| `main.go` | kiosk state, screen actions, timer, `main()` |
| `view.go` | 7 screens, all functions of state |
| `ident.go` | identity auto-detection rules (`ParseIdentifier`, `CleanIdentifier`) |
| `kiosk.go` | `kioskOptions` + `windowLockFor`/`applyWindowMode` (window locks and display mode) |
| `api.go` | APM HTTP client + contract types + `ModeForLookup`/`VerdictOf` |
| `mock.go` | invented data for `mock: true` |
| `config.go` | `config.json` and its validation |
| `log.go` | daily log `log/kiosk-YYYY-MM-DD.log` |
| `theme.go` | Kanagawa Lotus palette |
| `keypad.go` | on-screen numeric keyboard |

Network data never touches state from another goroutine: results are installed
through `Window.Update`, and answers that arrive after the patient has left are
discarded via the `gen` marker.

## Testing against the real server

Ordinary tests (`go test ./...`) use mocks and need no network. The
`contract_test.go` tests only run with `APM_LIVE=1` and a `config.json`
containing `mock: false` + a real API key:

```powershell
.\antrian.exe serve -addr :8080   # from the simpus-antrian-server repo
cd ..\touch
cmd /c "set ""APM_LIVE=1"" && go test -run TestLive -v ."
```

The four tests that run:

| Test | What it checks | Writes to DB? |
|---|---|---|
| `TestLiveContract` | shape of `options`, `poli`, `dokter`, `lookup`, queue `dry_run` | no |
| `TestLiveUI` | the 7 screens run with real data, stopping at the summary | no |
| `TestLiveQueueBody` | the kiosk's own `queueBody()` body is accepted by the server, `dry_run` | no |
| `TestLiveRegistrationBody` | the `mode=registration` fields are read (an invented number is answered with `PATIENT_NOT_FOUND`) | no |

No test presses "Ambil Nomor Antrian", so no queue row is actually issued. When
done, return `config.json` to `mock: true`.

`QUOTA_EXCEEDED` on a particular polyclinic is a legitimate business error (the
doctor's quota is used up), not a contract bug — that is why the tests try the
polyclinics one at a time.

## Running against the real server (build)

`go tool mygo build` produces `build/windows-amd64/simpus-antrian-interactive.exe`.
Copy `config.json` next to the exe — the kiosk reads it from the exe folder
first, or point `CONFIG_FILE` at it.

```powershell
cd touch
go tool mygo build
Copy-Item config.json build\windows-amd64\config.json
.\build\windows-amd64\simpus-antrian-interactive.exe
```

`--out` can send the build somewhere else, and `CONFIG_FILE` can point at the
config instead of copying it.

`dry_run` **must** be `true` during testing. That way the queue number is
computed by the server from real data, but no row is written to the database and
the number screen shows the **MODE UJI** warning. Set `"dry_run": false` only
once the kiosk is at the registration desk and its numbers should be recorded
in SIMPUS.

## Releases and CI

`.github/workflows/ci.yml` runs on every push to `main` and every pull request:
`gofmt -l .`, `go vet ./...`, `go tool mygo vet .`, `go tool mygo build`, and
`go test ./...`. Everything runs on `windows-latest` because MyGo draws Win32
windows through purego; there is no portable build. The four `TestLive*` tests
skip themselves without `APM_LIVE=1`, so CI needs no API key.

`.github/workflows/release.yml` runs when a `v*` tag is pushed. It refuses to
publish if the tag does not match `version` in `mygo.json`, then attaches:

| Asset | Contents |
|---|---|
| `simpus-antrian-interactive-<version>-windows-amd64.zip` | exe, `config.example.json`, `README.md`, `LICENSE` |
| `simpus-antrian-interactive-setup-<version>-amd64.exe` | installer |
| `SHA256SUMS.txt` | checksums for both |

The zip is the recommended download. It is the only asset that runs on its
own: unpack it, copy `config.example.json` to `config.json`, fill in the key,
and start the exe. A bare exe is never published, because without a
`config.json` beside it the kiosk refuses to start.

Verify a download before copying it to a kiosk:

```powershell
Get-FileHash .\simpus-antrian-interactive-0.1.0-windows-amd64.zip -Algorithm SHA256
```

```powershell
git tag v0.1.0
git push origin v0.1.0
```

Bump `version` in `mygo.json` before tagging; the zip and installer names come
from that field.

## License

MIT — see `LICENSE`. Copyright (c) 2026 SonyPradana.