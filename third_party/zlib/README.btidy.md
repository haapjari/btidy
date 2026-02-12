# Vendored zlib Sources

- This directory contains vendored files from zlib for Deflate64 support.
- (`archive/zip` method 9) via `contrib/infback9`.

- Upstream Project: https://github.com/madler/zlib
- Upstream Version: v1.3.1
- License: zlib (see `third_party/zlib/LICENSE`)

Files Used:

- `zlib.h`
- `zconf.h`
- `zutil.h`
- `contrib/infback9/infback9.c`
- `contrib/infback9/infback9.h`
- `contrib/infback9/inflate9.h`
- `contrib/infback9/inftree9.c`
- `contrib/infback9/inftree9.h`
- `contrib/infback9/inffix9.h`

- These files are consumed by `pkg/deflate64` through CGO glue code.

Integrity and license notes:

- The vendored zlib files listed above are copied verbatim from upstream tag `v1.3.1`.
- Verify byte-for-byte identity with `make verify-third-party`.
- If any vendored zlib file is modified, the change must be clearly marked as altered
  to satisfy the zlib LICENSE terms.


