#!/bin/zsh
# JOEY 发版脚本(NAS 自更新线) —— 在 GitHub CI 构建完成后执行。
# 做五件事:
#   ①从 GitHub Release 下载各平台包,抽出裸二进制(自更新按裸二进制替换自身)
#   ②用 build/release-history 里的历史 Windows 全量包生成 bsdiff 增量补丁
#   ③生成 /update/manifest.json 版本清单
#   ④上传 二进制+补丁+清单 到 NAS data/update/,并归档新全量包到 release-history
#   ⑤重建下载页两个 zip(免脚本新结构)并上传 data/dist/
# 用法: scripts/release-nas.sh 1.0.19 "更新说明"
set -e
export COPYFILE_DISABLE=1  # 防 macOS tar 打包 ._AppleDouble 垃圾
VER="$1"; NOTES="${2:-新版本 $VER}"
[ -z "$VER" ] && { echo "用法: $0 <版本号如1.0.19> [更新说明]"; exit 2; }
REPO="xiaojun0819a/JOEY"
GH="$HOME/.local/bin/gh"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK=$(mktemp -d)
SSH_OPTS=(-i "$HOME/.ssh/nas_deploy" -p 22122 -o IdentitiesOnly=yes)
NAS="admin@192.168.1.4"
export NO_PROXY=192.168.1.4

echo "== ① 下载 GitHub Release v$VER 并抽二进制 =="
cd "$WORK"
for a in JOEY_darwin_arm64.zip JOEY_darwin_amd64.zip JOEY_windows_amd64.zip; do
  "$GH" release download "v$VER" -R "$REPO" -p "$a" -D "$WORK"
done
unzip -q JOEY_darwin_arm64.zip -d m_arm && cp m_arm/JOEY.app/Contents/MacOS/jcp "JOEY-darwin-arm64-$VER"
unzip -q JOEY_darwin_amd64.zip -d m_amd && cp m_amd/JOEY.app/Contents/MacOS/jcp "JOEY-darwin-amd64-$VER"
unzip -q JOEY_windows_amd64.zip -d w_amd && cp w_amd/JOEY.exe "JOEY-win-amd64-$VER.exe"
ls -la JOEY-*-$VER*

echo "== ② 生成 Windows 增量补丁(有历史全量包才生成) =="
PATCH_JSON=""
for old in "$ROOT"/build/release-history/JOEY-win-amd64-*.exe; do
  [ -e "$old" ] || continue
  ov=$(basename "$old" .exe); ov=${ov#JOEY-win-amd64-}
  [ "$ov" = "$VER" ] && continue
  p="JOEY-win-amd64-$ov-to-$VER.patch"
  (cd "$ROOT" && go run ./tools/makepatch "$old" "$WORK/JOEY-win-amd64-$VER.exe" "$WORK/$p")
  PATCH_JSON="$PATCH_JSON\"$ov\":\"$p\","
done
PATCH_JSON=${PATCH_JSON%,}

echo "== ③ 生成 manifest.json =="
cat > manifest.json << JSON
{
  "version": "$VER",
  "notes": "$NOTES",
  "assets": {
    "darwin/arm64": "JOEY-darwin-arm64-$VER",
    "darwin/amd64": "JOEY-darwin-amd64-$VER",
    "windows/amd64": "JOEY-win-amd64-$VER.exe"
  },
  "patches": { "windows/amd64": { $PATCH_JSON } }
}
JSON
cat manifest.json

echo "== ④ 上传 NAS data/update/ =="
tar cf - JOEY-*-$VER* *.patch manifest.json 2>/dev/null | ssh "${SSH_OPTS[@]}" "$NAS" "tar xf - -C /volume1/docker/jcp-backend/data/update/"
cp "JOEY-win-amd64-$VER.exe" "$ROOT/build/release-history/"

echo "== ⑤ 重建下载页包(免脚本结构) =="
cp JOEY_darwin_arm64.zip "JOEY-macOS版.zip"
mkdir -p JOEY && cp "JOEY-win-amd64-$VER.exe" JOEY/JOEY.exe
cat > JOEY/README.txt << 'TXT'
JOEY 桌面版 · Windows 使用说明
====================
1. 解压到任意文件夹(绿色免安装,删除即卸载)
2. 直接双击 JOEY.exe 打开
3. 首次打开会弹登录框:点「没有账号?注册一个」自己设账号密码,
   或输入管理员发给你的账号密码
4. 之后 90 天内免登录,可在任何网络使用
5. 若提示缺少 WebView2 运行时:到微软官网搜 "WebView2 Runtime"
   下载安装后重开(Win10/Win11 大多已自带)
TXT
zip -qr "JOEY-Windows版.zip" JOEY
tar cf - "JOEY-macOS版.zip" "JOEY-Windows版.zip" | ssh "${SSH_OPTS[@]}" "$NAS" "tar xf - -C /volume1/docker/jcp-backend/data/dist/"

echo "== 验证 =="
curl -s -m 10 "https://joey-app.junai.uk/update/manifest.json" | head -12
echo "完成: v$VER 已发布到 NAS 自更新线 + 下载页"
rm -rf "$WORK"
