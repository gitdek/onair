"""Draws the README's graphics, each in a light and a dark theme.

    python3 docs/assets/generate.py

Writes docs/brand/logo-animated{,-dark}.svg and docs/assets/how-it-works{,-dark}.svg.
The pictures are drawn from the numbers below, so the two themes can't drift
apart. Every animation rests in a complete picture and stops for a reader
who asks for reduced motion.
"""
import os

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
FONT = "-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif"

THEMES = {
    "": dict(ink="#1f2328", quiet="#59636e", line="#d0d7de", panel="#f6f8fa",
             sign="#16161a", bezel="#2c2c33", bar="#8c959f",
             safe="#1a7f37", safe_fill="#dafbe1", live="#cf222e", live_fill="#ffebe9"),
    "-dark": dict(ink="#e6edf3", quiet="#9198a1", line="#3d444d", panel="#151b23",
                  sign="#1f1f25", bezel="#3a3a44", bar="#656c76",
                  safe="#3fb950", safe_fill="#12261e", live="#f85149", live_fill="#2a1519"),
}
LAMP = "#ff3b30"


def logo(t):
    """A studio sign, lit while a recording is live, over a moving level meter."""
    bars = []
    heights = [10, 18, 30, 22, 38, 26, 44, 30, 20, 34, 48, 28, 16, 36, 24, 42, 30, 18, 26, 38, 22, 12, 28, 16]
    for i, h in enumerate(heights):
        x = 252 + i * 19.2
        bars.append(f'<rect class="bar" x="{x:.1f}" y="{266 - h / 2:.1f}" width="9" height="{h}" rx="4.5" '
                    f'fill="{t["bar"]}" style="animation-delay:-{(i * 0.137) % 1.6:.2f}s"/>')
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 960 320" role="img" aria-label="On Air: a studio sign, lit red while a recording is live">
  <style>
    .glow {{ animation: glow 2.6s ease-in-out infinite; }}
    .lamp {{ animation: lamp 2.6s ease-in-out infinite; }}
    .bar {{ transform-box: fill-box; transform-origin: center; animation: level 1.6s ease-in-out infinite; }}
    @keyframes glow {{ 0%, 100% {{ opacity: .55; }} 50% {{ opacity: .95; }} }}
    @keyframes lamp {{ 0%, 100% {{ opacity: .9; }} 50% {{ opacity: 1; }} }}
    @keyframes level {{ 0%, 100% {{ transform: scaleY(.45); }} 50% {{ transform: scaleY(1); }} }}
    @media (prefers-reduced-motion: reduce) {{ .glow, .lamp, .bar {{ animation: none; }} }}
  </style>
  <defs>
    <filter id="halo" x="-30%" y="-80%" width="160%" height="260%"><feGaussianBlur stdDeviation="22"/></filter>
    <filter id="soft" x="-10%" y="-30%" width="120%" height="160%"><feGaussianBlur stdDeviation="5"/></filter>
  </defs>
  <rect class="glow" x="250" y="52" width="460" height="150" rx="30" fill="{LAMP}" opacity=".7" filter="url(#halo)"/>
  <rect x="240" y="44" width="480" height="166" rx="26" fill="{t["sign"]}" stroke="{t["bezel"]}" stroke-width="3"/>
  <rect x="256" y="60" width="448" height="134" rx="16" fill="none" stroke="{t["bezel"]}" stroke-width="1.5"/>
  <g font-family="{FONT}" font-weight="800" font-size="92" text-anchor="middle" letter-spacing="14">
    <text class="glow" x="487" y="160" fill="{LAMP}" filter="url(#soft)">ON AIR</text>
    <text class="lamp" x="487" y="160" fill="{LAMP}">ON AIR</text>
  </g>
  {"".join(bars)}
</svg>
'''


def how(t):
    """A recording as pieces on disk, and what happens to them at Stop."""
    ink, quiet, line = t["ink"], t["quiet"], t["line"]
    pieces = []
    for i in range(11):
        x = 250 + i * 62
        pieces.append(f'<rect class="piece" x="{x}" y="64" width="52" height="40" rx="8" fill="{t["safe_fill"]}" '
                      f'stroke="{t["safe"]}" stroke-width="1.5" style="animation-delay:{i * 0.35:.2f}s"/>')
    open_x = 250 + 11 * 62
    def box(x, w, title, sub):
        return (f'<rect x="{x}" y="232" width="{w}" height="76" rx="10" fill="{t["panel"]}" stroke="{line}" stroke-width="1.5"/>'
                f'<text x="{x + w / 2}" y="264" text-anchor="middle" font-size="21" font-weight="600" fill="{ink}">{title}</text>'
                f'<text x="{x + w / 2}" y="291" text-anchor="middle" font-size="17" fill="{quiet}">{sub}</text>')
    def arrow(x1, x2):
        return f'<path d="M{x1} 270H{x2}" stroke="{quiet}" stroke-width="1.5" marker-end="url(#tip)"/>'
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 348" role="img" aria-label="While recording, picture and sound go to disk in short pieces that each play on their own, so a crash costs only the open piece. At Stop the last piece is closed, the pieces are joined, and the finished file is handed over whole.">
  <style>
    .piece {{ animation: land 6s ease-in-out infinite; }}
    .open {{ animation: write 1.2s ease-in-out infinite; }}
    @keyframes land {{ 0%, 9%, 100% {{ opacity: 1; }} 4% {{ opacity: .35; }} }}
    @keyframes write {{ 0%, 100% {{ opacity: .55; }} 50% {{ opacity: 1; }} }}
    @media (prefers-reduced-motion: reduce) {{ .piece, .open {{ animation: none; }} }}
  </style>
  <defs>
    <marker id="tip" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" fill="{quiet}"/></marker>
  </defs>
  <g font-family="{FONT}">
    <text x="40" y="80" font-size="21" font-weight="600" fill="{ink}">While recording</text>
    <text x="40" y="105" font-size="17" fill="{quiet}">a few seconds each</text>
    {"".join(pieces)}
    <rect class="open" x="{open_x}" y="64" width="52" height="40" rx="8" fill="{t["live_fill"]}" stroke="{t["live"]}" stroke-width="1.5" stroke-dasharray="6 4"/>
    <path d="M250 122H922" stroke="{t["safe"]}" stroke-width="1.5"/>
    <path d="M250 116V128M922 116V128" stroke="{t["safe"]}" stroke-width="1.5"/>
    <text x="586" y="150" text-anchor="middle" font-size="17" fill="{t["safe"]}">closed: safe on disk, and each one plays on its own</text>
    <text x="{open_x + 26}" y="150" text-anchor="middle" font-size="17" fill="{t["live"]}">open:</text>
    <text x="{open_x + 26}" y="173" text-anchor="middle" font-size="17" fill="{t["live"]}">all a crash costs</text>
    <path d="M40 200H1160" stroke="{line}" stroke-width="1" stroke-dasharray="2 6"/>
    <text x="40" y="264" font-size="21" font-weight="600" fill="{ink}">At Stop</text>
    <text x="40" y="291" font-size="17" fill="{quiet}">about a second</text>
    {box(250, 250, "Close the last piece", "nothing else to save")}
    {arrow(500, 532)}
    {box(534, 190, "Join the pieces", "into one file")}
    {arrow(724, 756)}
    {box(758, 230, "Hand it over", "hidden until it is whole")}
    {arrow(988, 1020)}
    {box(1022, 150, "The archive", "on its shelf")}
  </g>
</svg>
'''


for suffix, theme in THEMES.items():
    for path, draw in ((f"docs/brand/logo-animated{suffix}.svg", logo), (f"docs/assets/how-it-works{suffix}.svg", how)):
        with open(os.path.join(ROOT, path), "w") as f:
            f.write(draw(theme))
        print("wrote", path)
