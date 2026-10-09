#!/usr/bin/env python3
"""Generate original AgentDeck vector-like launcher artwork (Pillow, dev only).
Coordinates match docs/brand/agentdeck-mark.svg. No downloaded brand assets.
"""
from pathlib import Path
import json
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1]
INK = '#191C20'
PAPER = '#F4F5F6'
ACCENT = '#EF633C'
SCALE = 4

def artwork(*, foreground=False, square=False):
    im = Image.new('RGBA', (1024*SCALE, 1024*SCALE), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    fit = .88 if foreground else 1
    def coord(v): return int((512 + (v - 512) * fit) * SCALE)
    def rect(box, radius, **kw):
        d.rounded_rectangle(tuple(coord(v) for v in box), radius=int(radius*SCALE*fit), **kw)
    def line(points, color, width):
        xy = [(coord(x), coord(y)) for x, y in points]
        d.line(xy, fill=color, width=int(width*SCALE*fit), joint='curve')
        for x, y in xy:
            radius=width*SCALE*fit/2
            d.ellipse((x-radius,y-radius,x+radius,y+radius),fill=color)
    if not foreground:
        rect((0,0,1024,1024),0 if square else 220,fill=INK)
    # Three decks. Quiet rear layers and a clear terminal on the front card.
    rect((320,336,800,800),72,outline='#6B7079',width=int(28*SCALE*fit))
    rect((280,292,760,756),72,outline='#B0B5BF',width=int(28*SCALE*fit))
    rect((224,224,704,688),72,fill=PAPER)
    line([(330,364),(416,448),(330,532)],INK,38)
    line([(484,532),(604,532)],ACCENT,38)
    return im

def save(im, path, size, opaque=False):
    path.parent.mkdir(parents=True, exist_ok=True)
    im=im.resize((size,size),Image.Resampling.LANCZOS)
    if opaque: im=im.convert('RGB')
    im.save(path,optimize=True)

legacy=artwork()
save(legacy,ROOT/'app/assets/brand/agentdeck-mark.png',256)
for density,size in [('mdpi',48),('hdpi',72),('xhdpi',96),('xxhdpi',144),('xxxhdpi',192)]:
    for name in ['ic_launcher', 'ic_launcher_round']:
        save(legacy,ROOT/f'app/android/app/src/main/res/mipmap-{density}/{name}.png',size)
save(artwork(foreground=True),ROOT/'app/android/app/src/main/res/drawable-nodpi/agentdeck_icon_foreground.png',432)
appicons=ROOT/'app/ios/Runner/Assets.xcassets/AppIcon.appiconset'
for entry in json.loads((appicons/'Contents.json').read_text())['images']:
    size=round(float(entry['size'].split('x')[0])*float(entry['scale'].rstrip('x')))
    save(artwork(square=True),appicons/entry['filename'],size,opaque=True)
print('Original brand artwork generated for Flutter, Android and iOS')
