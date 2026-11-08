# 布谷的完整沙箱镜像：运行代码、执行命令（run_python / run_shell）用。
# 在 python:3.12-slim 的基础上预装处理文档、图片、音视频常用的程序和 Python 库，大约 1.5GB。
#
# 用法：管理后台 → 系统设置 → 工具 → 运行代码的镜像，填 ghcr.io/hugehardjiang/buugoo-sandbox:latest。
# 不填就用默认的 python:3.12-slim（小，但没有这些程序）。
#
# 本地构建：docker build -t buugoo-sandbox deploy/sandbox
# 国内网络可以换软件源：--build-arg DEBIAN_MIRROR=mirrors.tuna.tsinghua.edu.cn --build-arg PIP_INDEX=https://pypi.tuna.tsinghua.edu.cn/simple
FROM python:3.12-slim

ARG DEBIAN_MIRROR=""
ARG PIP_INDEX=https://pypi.org/simple

# 预装的程序：
#   办公文档转换（Word / Excel / PPT → PDF 等）：LibreOffice
#   文档格式转换、PDF 工具：pandoc、poppler-utils、qpdf、ghostscript
#   图片、音视频：ImageMagick、ffmpeg
#   中文和常用字体（图表、PDF、Office 文档里的中文不会变成方框）：Noto CJK、Noto Emoji、DejaVu
#   常用命令行工具和 JavaScript：Node.js、npm、jq、zip、sqlite3、git、curl 等
RUN set -eux; \
    if [ -n "$DEBIAN_MIRROR" ]; then sed -i "s|deb.debian.org|$DEBIAN_MIRROR|g" /etc/apt/sources.list.d/debian.sources; fi; \
    apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      libreoffice-writer-nogui libreoffice-calc-nogui libreoffice-impress-nogui \
      pandoc poppler-utils qpdf ghostscript \
      imagemagick ffmpeg \
      fonts-noto-cjk fonts-noto-color-emoji fonts-dejavu-core \
      nodejs npm jq zip unzip xz-utils file bc sqlite3 git curl ca-certificates; \
    apt-get clean; rm -rf /var/lib/apt/lists/* /usr/share/doc/* /usr/share/man/*; \
    fc-cache -f

# 常用 Python 库（数据分析、画图、读写 Office / PDF、图片处理）。
RUN pip install --no-cache-dir -i "$PIP_INDEX" \
      numpy scipy sympy pandas mpmath matplotlib seaborn \
      openpyxl xlrd python-docx python-pptx reportlab pypdf pdfplumber \
      pillow beautifulsoup4 lxml markdown tabulate jinja2 requests chardet

# 布谷以 nobody（65534）身份、只读根目录运行容器，HOME 在 /tmp。
ENV HOME=/tmp MPLCONFIGDIR=/tmp/mpl LANG=C.UTF-8
LABEL org.opencontainers.image.title="buugoo-sandbox" \
      org.opencontainers.image.description="布谷运行代码的沙箱：Python + LibreOffice、pandoc、ffmpeg、ImageMagick、Node.js、中文字体"
