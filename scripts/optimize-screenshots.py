#!/usr/bin/env python3
"""Turn Playwright PNGs and the Monatsexport PDF into docs/screenshots WebPs."""

import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

try:
    from PIL import Image
except ImportError:
    print("python3-pil is required (Pillow with WebP)", file=sys.stderr)
    sys.exit(1)

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / "e2e" / "screenshots"
OUT = ROOT / "docs" / "screenshots"
NAMES = (
    "login",
    "heute",
    "erfassen",
    "pruefen",
    "monat",
    "monatsexport",
    "jahresregel",
    "einstellungen",
)
VIEWS = ("desktop", "mobile")
MAX_W = {"desktop": 1100, "mobile": 430}
BUDGET = 1_900_000


def fit(image: Image.Image, max_width: int) -> Image.Image:
    if image.width <= max_width:
        return image
    height = round(image.height * max_width / image.width)
    return image.resize((max_width, height), Image.Resampling.LANCZOS)


def save_webp(image: Image.Image, dest: Path, quality: int) -> None:
    if image.mode not in ("RGB", "RGBA"):
        image = image.convert("RGBA")
    image.save(dest, "WEBP", quality=quality, method=6)


def raster_pdf(pdf: Path, dest_png: Path) -> None:
    if shutil.which("pdftoppm") is None:
        print("pdftoppm is required (poppler-utils)", file=sys.stderr)
        sys.exit(1)
    dest_png.parent.mkdir(parents=True, exist_ok=True)
    stem = dest_png.with_suffix("")
    subprocess.run(
        ["pdftoppm", "-png", "-r", "120", "-f", "1", "-l", "1", "-singlefile", str(pdf), str(stem)],
        check=True,
    )
    if not dest_png.is_file():
        print(f"pdftoppm wrote no {dest_png}", file=sys.stderr)
        sys.exit(1)


def encode(quality: int, page_png: Path) -> int:
    if OUT.exists():
        for old in OUT.glob("*.webp"):
            old.unlink()
    OUT.mkdir(parents=True, exist_ok=True)
    total = 0
    for view in VIEWS:
        for name in NAMES:
            with Image.open(SRC / view / f"{name}.png") as image:
                fitted = fit(image, MAX_W[view])
                dest = OUT / f"{view}-{name}.webp"
                save_webp(fitted, dest, quality)
                total += dest.stat().st_size
    with Image.open(page_png) as image:
        fitted = fit(image, 1400)
        dest = OUT / "monatsexport-page.webp"
        save_webp(fitted, dest, quality)
        total += dest.stat().st_size
    return total


def main() -> int:
    missing = []
    for view in VIEWS:
        for name in NAMES:
            path = SRC / view / f"{name}.png"
            if not path.is_file():
                missing.append(str(path))
    pdf = SRC / "monatsexport.pdf"
    if not pdf.is_file():
        missing.append(str(pdf))
    if missing:
        print("missing screenshot inputs:\n" + "\n".join(missing), file=sys.stderr)
        return 1

    with tempfile.TemporaryDirectory() as tmp:
        page_png = Path(tmp) / "page.png"
        raster_pdf(pdf, page_png)
        total = 0
        for quality in (74, 60, 48):
            total = encode(quality, page_png)
            print(f"quality {quality}: {total} bytes")
            if total <= BUDGET:
                break
        else:
            print(f"docs/screenshots is {total} bytes, over {BUDGET}", file=sys.stderr)
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
