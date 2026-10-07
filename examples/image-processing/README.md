# Resample example

This example loads `source_720p.svg` at 1280x720 and creates three outputs:

- `output_1080p.png`: enlarged to 1920x1080 with `Image.Resize` and Lanczos3.
- `output_thumbnail_64.png`: a centered, exact 64x64 thumbnail.
- `output_rotate_flip.png`: a 90-degree rotation followed by a horizontal flip,
  combined into one affine transform before resampling.

Build and run from this directory:

```text
make run
```

## Selecting the native libvips SKU

The blank import below activates the `shared` package defined by the
dependency module's `hike.mod`:

```hike
// Blank-import shared to drive the native build pipeline and copy the
// required libvips DLLs beside the executable automatically.
import _ "github.com/kanryu/hike-images-vips/shared"
```

Importing `shared` causes the link and asset resources declared for that
package in `hike.mod` to be linked and copied during the build. End users can
switch the blank import to another package such as `static` or `debug` to
select a different SKU of the same native library without changing the image
processing code.

The example is deliberately format-agnostic: changing the filename suffixes
is enough to exercise another libvips loader or saver. Change `inputPath` in
`main.hike` when using a different fixture.
