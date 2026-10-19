"""Export the approved metallic bird without redrawing or recoloring it.

Developer tool only: python3 -m pip install Pillow==12.3.0
Run from any directory: python3 assets/branding/export.py
The application and its builds consume the committed exports, not Pillow.
"""

from pathlib import Path
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[2]
BRAND = ROOT / "assets/branding"
WEB = ROOT / "frontend/src/assets/branding"
WEB.mkdir(parents=True, exist_ok=True)
bird = Image.open(BRAND / "bird-metallic-1024.png").convert("RGBA")
assert bird.size == (1024, 1024)


def tile(inset, radius):
    # A light, rounded plate preserves the metallic bird on dark desktops.
    # macOS has extra transparent space around the plate, like other Dock icons.
    image = Image.new("RGBA", (1024, 1024))
    ImageDraw.Draw(image).rounded_rectangle(
        (inset, inset, 1023 - inset, 1023 - inset), radius, fill="white"
    )
    size = 1024 - 2 * inset
    image.alpha_composite(bird.resize((size, size), Image.Resampling.LANCZOS), (inset, inset))
    return image


windows = tile(0, 180)
macos = tile(100, 184)
macos.save(BRAND / "appicon-macos.png")
windows.save(BRAND / "appicon-windows.ico", sizes=[(n, n) for n in (16, 24, 32, 48, 64, 128, 256)])
windows.resize((128, 128), Image.Resampling.LANCZOS).save(WEB / "brandmark-128.png")
windows.save(WEB / "favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)])
print("Exported metallic macOS, Windows, web and favicon assets.")
