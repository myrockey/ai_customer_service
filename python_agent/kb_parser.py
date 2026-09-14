"""知识库文件解析：按扩展名提取纯文本。

支持：txt / md / csv / pdf / docx / xlsx
图片 OCR 暂不支持（返回明确错误）。
解析库懒加载，未安装时给出清晰错误提示。
"""
import io
import logging

logger = logging.getLogger("agent.kb_parser")

# 各扩展名 → 解析函数名
SUPPORTED_EXTS = {".txt", ".md", ".csv", ".pdf", ".docx", ".xlsx"}


def _load_pypdf():
    try:
        from pypdf import PdfReader
        return PdfReader
    except ImportError:
        raise RuntimeError("服务器缺少 pypdf 依赖，无法解析 PDF")


def _load_python_docx():
    try:
        from docx import Document
        return Document
    except ImportError:
        raise RuntimeError("服务器缺少 python-docx 依赖，无法解析 DOCX")


def _load_openpyxl():
    try:
        from openpyxl import load_workbook
        return load_workbook
    except ImportError:
        raise RuntimeError("服务器缺少 openpyxl 依赖，无法解析 XLSX")


def extract_text(filename: str, raw: bytes) -> str:
    """按扩展名提取文本。不支持的格式抛 RuntimeError（带明确原因）。"""
    name = (filename or "").lower()
    # 无扩展名：按 UTF-8 文本尝试
    if "." not in name:
        return raw.decode("utf-8", errors="ignore").strip()
    ext = "." + name.rsplit(".", 1)[1]
    if ext not in SUPPORTED_EXTS:
        raise RuntimeError(
            f"暂不支持 {ext} 格式，目前支持: txt / md / csv / pdf / docx / xlsx")
    try:
        if ext in (".txt", ".md", ".csv"):
            return raw.decode("utf-8", errors="ignore").strip()
        if ext == ".pdf":
            return _extract_pdf(raw)
        if ext == ".docx":
            return _extract_docx(raw)
        if ext == ".xlsx":
            return _extract_xlsx(raw)
    except RuntimeError:
        raise
    except Exception as e:
        logger.exception("parse %s error", ext)
        raise RuntimeError(f"{ext} 解析失败: {e}")
    return ""


def _extract_pdf(raw: bytes) -> str:
    PdfReader = _load_pypdf()
    reader = PdfReader(io.BytesIO(raw))
    parts = []
    for page in reader.pages:
        try:
            t = page.extract_text() or ""
        except Exception:
            t = ""
        if t.strip():
            parts.append(t)
    text = "\n".join(parts).strip()
    if not text:
        raise RuntimeError("PDF 无可提取文本（可能为扫描件/图片型 PDF，暂不支持 OCR）")
    return text


def _extract_docx(raw: bytes) -> str:
    Document = _load_python_docx()
    doc = Document(io.BytesIO(raw))
    parts = [p.text for p in doc.paragraphs if p.text.strip()]
    for table in doc.tables:
        for row in table.rows:
            cells = [c.text.strip() for c in row.cells if c.text.strip()]
            if cells:
                parts.append(" | ".join(cells))
    text = "\n".join(parts).strip()
    if not text:
        raise RuntimeError("DOCX 无可提取文本")
    return text


def _extract_xlsx(raw: bytes) -> str:
    load_workbook = _load_openpyxl()
    wb = load_workbook(io.BytesIO(raw), read_only=True, data_only=True)
    parts = []
    for ws in wb.worksheets:
        for row in ws.iter_rows(values_only=True):
            cells = [str(c).strip() for c in row if c is not None and str(c).strip()]
            if cells:
                parts.append(" | ".join(cells))
    text = "\n".join(parts).strip()
    if not text:
        raise RuntimeError("XLSX 无可提取文本")
    return text
