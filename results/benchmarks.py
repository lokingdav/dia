from pathlib import Path

import pandas as pd

base_dir = Path(__file__).resolve().parent
mobile_csv = base_dir / "roles-mb.csv"
desktop_csv = base_dir / "roles-pc.csv"

def op_label(name: str) -> str:
    parts = str(name).split()
    if not parts:
        return str(name)
    head = parts[0]
    tail = " ".join(p.capitalize() for p in parts[1:])
    macro_acronyms = {"AKE", "RUA", "IAM", "ODA"}
    if head.upper() in macro_acronyms:
        return rf"\{head.upper()} {tail}".strip()
    return " ".join([head] + [p.capitalize() for p in parts[1:]])

def fmt_ms(mean_ms, mad_ms, samples):
    if mean_ms is None:
        return "-"
    # Always show MAD when present so Desktop prints "median (MAD)" too.
    if mad_ms is not None:
        return f"{float(mean_ms):.2f} ({float(mad_ms):.3f})"
    return f"{float(mean_ms):.2f}"

def total_bytes(row):
    if row is None:
        return None
    sent = row.get("bytes_sent")
    recv = row.get("bytes_received")
    if sent is None or recv is None:
        return None
    return int(sent) + int(recv)

df_mb = pd.read_csv(mobile_csv)
df_pc = pd.read_csv(desktop_csv)

for col in ["samples", "bytes_sent", "bytes_received", "median_ms", "mad_ms"]:
    if col in df_mb.columns:
        df_mb[col] = pd.to_numeric(df_mb[col], errors="coerce")
    if col in df_pc.columns:
        df_pc[col] = pd.to_numeric(df_pc[col], errors="coerce")

df_mb = df_mb.set_index("name")
df_pc = df_pc.set_index("name")

# Prefer mobile ordering; append desktop-only entries at the end.
ordered_names = list(df_mb.index) + [n for n in df_pc.index if n not in df_mb.index]


def row_dict(df, name):
    if name not in df.index:
        return None
    r = df.loc[name]
    return {
        "samples": None if pd.isna(r.get("samples")) else int(r.get("samples")),
        "bytes_sent": None if pd.isna(r.get("bytes_sent")) else int(r.get("bytes_sent")),
        "bytes_received": None if pd.isna(r.get("bytes_received")) else int(r.get("bytes_received")),
        "median_ms": None if pd.isna(r.get("median_ms")) else float(r.get("median_ms")),
        "mad_ms": None if pd.isna(r.get("mad_ms")) else float(r.get("mad_ms")),
    }

lines = []
lines.append(r"\begin{tabular}{|l|c|c|c|}")
lines.append(r"\hline")
lines.append(r"\textbf{Operation} & \textbf{Mobile} & \textbf{Desktop} & \textbf{Total Bytes} \\ \hline")

for name in ordered_names:
    m = row_dict(df_mb, name)
    d = row_dict(df_pc, name)

    m_mean = None if m is None else m.get("median_ms")
    m_mad = None if m is None else m.get("mad_ms")
    m_samples = None if m is None else m.get("samples")

    d_mean = None if d is None else d.get("median_ms")
    d_mad = None if d is None else d.get("mad_ms")
    d_samples = None if d is None else d.get("samples")
    tb = total_bytes(m)
    if tb is None:
        tb = total_bytes(d)

    lines.append(
        " ".join(
            [
                f"{op_label(name)}",
                "&",
                fmt_ms(m_mean, m_mad, m_samples),
                "&",
                fmt_ms(d_mean, d_mad, d_samples),
                "&",
                str(tb) if tb is not None else "-",
                r"\\ \hline",
            ]
        )
    )

lines.append(r"\end{tabular}")

print("\n", "\n".join(lines))