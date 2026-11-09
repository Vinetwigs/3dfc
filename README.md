# 3dfc

**Store render frames once. Restore them whenever you need them.**

3dfc is a lossless archive tool for numbered PNG sequences. It combines a
terminal interface with a scriptable command line, and can restore a whole
sequence, a single frame, or any inclusive range. Restored pixels, including
alpha, are preserved exactly.

## Highlights

- Archive a PNG sequence into one `.3dfc` file.
- Restore all frames or select a frame, prefix, or range.
- Preserve original frame filenames and transparency.
- Check an archive before relying on it with `verify`.
- Compare compression options with `benchmark`.
- Use the interactive terminal interface or the command line.

## Get started

Build with Go 1.24 or later:

```sh
go build -o 3dfc ./cmd/3dfc
```

On Windows, the output is `3dfc.exe`. Run it without arguments to open the
terminal interface. In PowerShell, use `.\3dfc.exe`; replace `./3dfc` in
the examples below.

```sh
./3dfc archive -in ./renders/shot01 -out ./archive/shot01.3dfc
./3dfc verify -in ./archive/shot01.3dfc
./3dfc extract -in ./archive/shot01.3dfc -out ./restored --all
```

By default, 3dfc will not replace an existing archive or restored frame.
Use `-force` for an existing archive or `-overwrite` when extracting only
after confirming that replacement is intended.

The archive command reports the source size, archive size, and space saved.
Use `info` to view those figures later:

```sh
./3dfc info -in ./archive/shot01.3dfc
```

## Restore frames

Frame numbers start at `0`. Range endpoints are inclusive. If no selector is
provided, `extract` restores the full sequence.

```sh
# Restore one frame
./3dfc extract -in shot01.3dfc -out ./restore --frame 1422

# Restore frames 0 through 100
./3dfc extract -in shot01.3dfc -out ./restore --upto 100

# Restore frames 1400 through 1450
./3dfc extract -in shot01.3dfc -out ./restore --from 1400 --to 1450

# Restore every frame
./3dfc extract -in shot01.3dfc -out ./restore --all
```

Extracted PNGs keep their original filenames.
Numbered filenames are ordered naturally, so `frame2.png` comes before
`frame10.png`. Gaps in a numbered sequence are reported during archiving.
Extraction checks for existing files before it starts and leaves them alone
unless `-overwrite` is specified. Each restored file is written to a temporary
file and moved into place only after it is complete.

```sh
./3dfc extract -in shot01.3dfc -out ./restore --all --overwrite
```

## Compare compression options

Compression depends on the sequence. Benchmark a representative sample before
archiving a large project:

```sh
./3dfc benchmark -in ./renders/shot01 -preset fast -max 200
```

The benchmark compares the same frames with every available backend, verifies
each result by default, and prints archive size, space saved, encode time, and
throughput. The FFmpeg backend is included when FFmpeg is installed.

Useful options:

| Option | Default | Description |
|---|---|---|
| `-backends` | `all` | Compare all available backends or provide a comma-separated selection |
| `-preset` | `max` | Choose `max` for smaller archives or `fast` for quicker encoding |
| `-max` | `0` | Limit the benchmark to the first N frames; `0` uses all frames |
| `-verify` | `true` | Verify each trial archive |
| `-keep` | `false` | Keep trial archives instead of removing them |

## Archive options

```sh
./3dfc archive -in ./renders/shot01 -out ./archive/shot01.3dfc \
  -backend auto -preset max -gop 300 -tile 64 -fps 24
```

| Option | Default | Description |
|---|---|---|
| `-backend` | `auto` | Choose `auto`, `go`, or `ffmpeg` |
| `-preset` | `max` | Choose `max` for smaller archives or `fast` for quicker encoding |
| `-gop` | `300` | Maximum frames between keyframes. Smaller values speed up isolated frame restores |
| `-tile` | `64` | Tile size used by the Go backend |
| `-fps` | `24` | Frame rate stored with FFmpeg segments |
| `-force` | `false` | Replace an existing archive after successful encoding |
| `-resume` | `false` | Save checkpoints and continue an interrupted archive on a later run |

The `auto` backend selects a suitable method for each part of the sequence.
FFmpeg is optional. When available, it can improve compression for footage
with substantial motion. It is also required to restore or verify archives
that contain FFmpeg-encoded segments.

For long jobs, enable checkpoints on the first run and use the same command
again after an interruption. Checkpoints are saved at completed GOP boundaries:

```sh
./3dfc archive -in ./renders/shot01 -out ./archive/shot01.3dfc -resume
```

Resume checks the source frames and encoding options before continuing. Keep
the source sequence unchanged until the archive completes and passes `verify`.
While incomplete, resume state is kept next to the target archive and is removed
after a successful commit. Use `-force -resume` only to discard an incompatible
checkpoint and start over.

## Archiving in steps

For a long sequence, start with checkpoints enabled and rerun the same command
whenever the job stops. Each run continues from the last completed GOP:

```sh
./3dfc archive -in ./renders/shot01 -out ./archive/shot01.3dfc -resume
# interrupted? run the exact same command again
./3dfc archive -in ./renders/shot01 -out ./archive/shot01.3dfc -resume
```

1. Use the same source folder, output path, and options every time.
2. Do not modify, rename, or reorder the source frames between runs.
3. When the archive completes, run `verify` before deleting anything.

A checkpoint from different frames or different options is rejected instead of
resumed. To abandon it and start over, rerun once with `-force -resume`.

During archiving, progress is shown in two phases: scanning, then encoding.
Each phase counts the source frames once, so a 2,000-frame sequence completes
at `2000/2000` in each phase. The TUI and CLI identify the active phase.

## Interactive interface

Run `3dfc` with no arguments to open the TUI. It provides the same archive,
restore, benchmark, info, and verify operations as the command line.

| Key | Action |
|---|---|
| Up / Down | Move through the menu |
| 1-5 | Open a menu item directly |
| Tab | Move to the next form field |
| Left / Right | Change an option, or move the cursor in a text field |
| Enter | Continue or run the operation |
| Esc | Return to the menu |
| Q | Quit from the menu or result screen |

The archive and restore forms include `Overwrite` and `Resume` controls. For a
long render, set `Resume` to `yes` before starting. After an interruption,
choose the same source and output path and run it again with `Resume` enabled.

## Safe archiving

Before deleting source frames:

1. Create the archive.
2. Run `verify` and confirm it succeeds.
3. Copy the archive to a second storage device.
4. Restore and inspect a few frames.
5. Only then remove the original sequence.

Keep at least two independent copies of anything you cannot recreate.

## Notes

- Input is a directory of PNG frames with consistent image dimensions.
- Lossless encoding preserves pixel values. 16-bit PNGs are stored unchanged.
- Sequences that do not compress well are stored without applying a larger
  compressed representation. Small sequences can still gain a little overhead
  from archive metadata.
- Run `./3dfc --help` or `./3dfc <command> --help` for command help.

## Development

```sh
go test ./...
go vet ./...
```

## License

Copyright (C) 2026 Vinetwigs. Licensed under GPL-3.0-only, see `LICENSE`.