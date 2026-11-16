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

`config.json` is read from next to the executable; `CONFIG_FILE` can override
it, and `go run`/`mygo dev` fall back to the working folder.

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
refuses to start with a message naming which field is empty.

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
first.

```powershell
cd touch
go tool mygo build
Copy-Item config.json build\windows-amd64\config.json
.\build\windows-amd64\simpus-antrian-interactive.exe
```

`dry_run` **must** be `true` during testing. That way the queue number is
computed by the server from real data, but no row is written to the database and
the number screen shows the **MODE UJI** warning. Set `"dry_run": false` only
once the kiosk is at the registration desk and its numbers should be recorded
in SIMPUS.

## License

MIT — see `LICENSE`. Copyright (c) 2026 SonyPradana.