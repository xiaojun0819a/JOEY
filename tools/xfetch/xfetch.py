#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
X 荐股博主抓取端 —— 跑在那台常开的 Windows 笔记本上(它挂着 VPN,NAS 直连不到 x.com)。

它只做一件事:把博主动态的**原始文本**原样搬到 NAS,一行解析都不做。
解析规则以后天天要改,改在服务端就不用每次远程去动这台机器;
而且原文全量存档后,规则改了还能拿旧数据重跑一遍。

两种模式:
  poll     每隔几分钟刷一轮六个博主的主页(默认)
  backfill 往回翻历史推文,做半年回溯验证用

登录态:第一次运行会打开浏览器让你手动登录 X,登录信息存在 ./xsession 目录里,以后自动复用。
**未登录只能看到 7 条**,轮询够用,但 backfill 必须登录。

用法:
  python xfetch.py login                      # 只登录,存会话
  python xfetch.py poll                       # 常驻轮询(Ctrl+C 停)
  python xfetch.py poll --once                # 只跑一轮,给任务计划程序用
  python xfetch.py backfill --days 180        # 回溯半年
  python xfetch.py backfill --handle shachoo_king --days 180
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone

# ==== 配置(只有这一段需要改)=======================================
NAS_URL = os.environ.get("JCP_URL", "https://joey-app.junai.uk")
NAS_TOKEN = os.environ.get("JCP_TOKEN", "")          # 与 NAS 后端 /rpc 用的是同一个令牌
POLL_SECONDS = int(os.environ.get("XFETCH_INTERVAL", "300"))   # 轮询间隔,默认 5 分钟
# 本地代理,形如 http://127.0.0.1:7890。VPN 若已开 TUN/全局模式则留空即可,
# 不确定就先跑 `python xfetch.py doctor`,它会自己试出来。
PROXY = os.environ.get("XFETCH_PROXY", "").strip() or None
# 无头模式默认**关闭**。X 认得出 headless 浏览器,认出来就不渲染时间线,
# 表现是页面打得开、但永远等不到推文(实测六个博主全军覆没)。
# 这台笔记本反正一直开着,让它挂个浏览器窗口不碍事。要静默跑就设 XFETCH_HEADLESS=1。
HEADLESS = os.environ.get("XFETCH_HEADLESS", "").strip() in ("1", "true", "yes")

# 句柄必须与后端 internal/xblogger/parse.go 里的 Configs 一致。
# 后端对未登记的句柄会直接拒收 —— 解析规则是按人配的,不能通配。
HANDLES = [
    "GusQuijasTJ",    # 走上大A巅峰
    "Aw3ff_",         # A股趋势捕手
    "Ferhat31162",    # 老林A股-寻找主线
    "ComMurtadha",    # 山野寻龙A股
    "shachoo_king",   # A股老枪
    "naixiaiwangu",   # 云舒交易日记
]
# ===================================================================

# Windows 控制台默认用 GBK,直接 print 中文会 UnicodeEncodeError 崩掉整个脚本。
# 在 Python 里切 UTF-8 是安全的 —— 在 .bat 里写 chcp 65001 才是会让 cmd 丢失脚本的那个坑。
if sys.platform == "win32":
    try:
        os.system("chcp 65001 >nul")
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

SESSION_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "xsession")
SEEN_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "seen_ids.json")
CST = timezone(timedelta(hours=8))

# 版本号:每次改完都要手动拷到那台 Windows 上,
# 拷漏了文件的症状五花八门(比如 bat 换了、py 没换 → "invalid choice: test")。
# 启动时打一行版本,一眼就知道跑的是不是最新的。
VERSION = "2026-08-02.1"


def log(msg):
    print(f"[{datetime.now(CST).strftime('%m-%d %H:%M:%S')}] {msg}", flush=True)


# ---- 本地已发送记录 -------------------------------------------------
# 服务端本来就用 tweet_id 幂等,这份记录只是为了少发无用请求。
# 丢了也不会重复建仓,所以不做任何加锁/容错。
def load_seen():
    try:
        with open(SEEN_FILE, "r", encoding="utf-8") as f:
            return set(json.load(f))
    except Exception:
        return set()


def save_seen(seen):
    try:
        with open(SEEN_FILE, "w", encoding="utf-8") as f:
            json.dump(sorted(seen)[-5000:], f)
    except Exception as e:
        log(f"⚠️ 写 seen_ids.json 失败(不影响运行):{e}")


# ---- 上报 -----------------------------------------------------------
# NAS 在 Cloudflare 隧道后面,CF 的机器人检测会拿 User-Agent 当第一道判据。
# 用默认的 "Python-urllib/3.x" 会被直接判死:HTTP 403 error code 1010(浏览器完整性检查未通过)。
UA = ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

# 上报**不走 VPN**:
#   ① NAS 隧道国内直连得到,绕一圈境外反而慢;
#   ② 从境外出口 IP 打自己的域名,正是 CF 最爱拦的形态(和 UA 一起构成 1010)。
# Windows 上 urllib 会自动读注册表里的系统代理(Hiddify 设的那个),所以必须显式清空。
_direct_opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def post_to_nas(handle, posts, dry_run=False):
    if not posts:
        return None
    payload = {"handle": handle, "posts": posts}
    if dry_run:
        payload["dryRun"] = True
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        NAS_URL.rstrip("/") + "/x/ingest",
        data=body,
        headers={
            "Content-Type": "application/json; charset=utf-8",
            "X-JCP-Token": NAS_TOKEN,
            "User-Agent": UA,
            "Accept": "application/json",
        },
        method="POST",
    )
    # 先直连;直连不通(比如人在境外)再退回系统代理
    for send, how in ((_direct_opener.open, "直连"), (urllib.request.urlopen, "走代理")):
        try:
            with send(req, timeout=40) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            detail = e.read()[:200].decode("utf-8", "replace")
            if e.code == 403 and "1010" in detail:
                log(f"❌ {how} 被 Cloudflare 拦(1010):UA 或出口 IP 被判为机器人")
            else:
                log(f"❌ {how} 上报 HTTP {e.code}:{detail}")
        except Exception as e:
            log(f"❌ {how} 上报失败:{e}")
    return None


# ---- 抓取 -----------------------------------------------------------
def make_context(pw, headless=True):
    # 用 persistent context 而不是普通 context:X 的登录态包含大量 localStorage,
    # 只存 cookies 会隔几天就掉线,那样这台无人值守的机器就白跑了。
    #
    # ⚠️代理:Playwright 用的是它自己下载的那个 Chromium,不是系统里那个 Chrome。
    # 很多 VPN 客户端只改「系统代理」设置,这个独立 Chromium 经常不吃,
    # 表现就是页面一片空白、什么都打不开(x.com 和 google 一起打不开基本就是这个)。
    # 两条路:① VPN 开 TUN/虚拟网卡模式,全局接管,这里就不用配;
    #        ② 把本地代理端口填进 XFETCH_PROXY,由 Playwright 直接走。
    kw = dict(
        headless=headless,
        viewport={"width": 1280, "height": 1600},
        locale="zh-CN",
        timezone_id="Asia/Shanghai",
        args=["--disable-blink-features=AutomationControlled"],
    )
    if PROXY:
        kw["proxy"] = {"server": PROXY}
    return pw.chromium.launch_persistent_context(SESSION_DIR, **kw)


# 常见本地代理端口。doctor 会挨个试,省得让用户自己去翻客户端设置。
# Hiddify 放最前面 —— 用户用的就是它,而它的默认混合端口 12334 很特别,
# 不在其他客户端的惯用端口里(第一版漏了这个,结果六个端口全试失败)。
COMMON_PROXIES = [
    "http://127.0.0.1:12334",   # Hiddify(默认混合端口)
    "http://127.0.0.1:2334",    # Hiddify 老版本
    "http://127.0.0.1:7890",    # Clash / Clash Verge
    "http://127.0.0.1:7897",    # Clash Verge 新版
    "http://127.0.0.1:10809",   # v2rayN (http)
    "http://127.0.0.1:10808",   # v2rayN (socks)
    "http://127.0.0.1:1080",    # Shadowsocks
    "http://127.0.0.1:8080",
]


def check_login(page):
    """开跑前先确认登录态还在。

    没登录时 X 只渲染个人资料头部(简介、粉丝数、Joined),时间线一条不给 ——
    症状和"被限流"一模一样,不明说就只能靠猜。踩过的实例:把新版文件夹拖进了旧文件夹,
    变成 xfetch\\xfetch 嵌套,而 xsession 留在外层,于是内层脚本静默地以未登录身份运行。
    """
    try:
        page.goto("https://x.com/home", wait_until="domcontentloaded", timeout=40000)
        page.wait_for_timeout(3000)
        for sel in ('[data-testid="SideNav_AccountSwitcher_Button"]',
                    '[data-testid="AppTabBar_Profile_Link"]',
                    '[data-testid="primaryColumn"] [data-testid="tweet"]'):
            if page.query_selector(sel):
                return True
        body = (page.inner_text("body") or "")[:200].replace("\n", " ")
        log("❌ 看起来**没有登录** —— X 对未登录用户不渲染时间线,抓不到任何推文。")
        log(f"   页面显示:{body}")
        log(f"   登录态目录:{SESSION_DIR}")
        log("   若这个路径不对(比如多了一层 xfetch\\xfetch),把文件挪回外层再跑;")
        log("   否则运行 1-install.bat 重新登录一次。")
        return False
    except Exception as e:
        log(f"⚠️ 登录态自检失败(先继续跑):{str(e)[:100]}")
        return True


def show_parsed(pk):
    """把一条推文的解析结果按人能读的样子打出来。"""
    t = pk.get("target") or {}
    head = f"目标日 {t.get('date','?')}({t.get('basis','?')})"
    if t.get("sameDay"):
        head += " · 当日复盘不建仓"
    log(f"     {head}")
    for label, key in (("✅ 明日买入", "buys"), ("⚪️ 已持仓", "holds"), ("🔴 已卖出", "exits")):
        items = pk.get(key) or []
        if items:
            log(f"     {label}:" + "、".join(f"{i['name']}({i['symbol']})" for i in items))
    for pl in pk.get("plans") or []:
        bits = []
        if pl.get("entryLow"):
            bits.append(f"低吸 {pl['entryLow']}-{pl['entryHigh']}")
        if pl.get("tp1"):
            bits.append(f"止盈 {pl['tp1']}/{pl.get('tp2')}")
        if pl.get("stop"):
            bits.append(f"止损 {pl['stop']}")
        if bits:
            log(f"     📋 {pl['name']} " + " · ".join(bits))
    for w in pk.get("warnings") or []:
        log(f"     ⚠️ {w}")


def cmd_test(only_handle, want):
    """测试模式:不管日期抓最新的推文,逐条给你看解析成什么样。

    周末和节假日没人发新票,但功能得能验。所以这里**不做日期过滤**,
    抓到的都拿去解析;一轮里没解析出买入票就继续往前翻,直到凑够 want 条有票的。

    走 dryRun:只解析、不入库、不推送、不建仓 —— 测试不该在数据里留痕,
    也不该因为"这条已经存过"就跳过解析。
    """
    from playwright.sync_api import sync_playwright
    targets = [only_handle] if only_handle else HANDLES
    with sync_playwright() as pw:
        ctx = make_context(pw, headless=HEADLESS)
        page = ctx.pages[0] if ctx.pages else ctx.new_page()
        if not check_login(page):
            ctx.close()
            return
        for h in targets:
            log(f"══ @{h} ══")
            posts = []
            for scrolls in (3, 12, 25):   # 抓到了但不够多,就往回翻更多
                posts = scrape_handle(page, h, want_after=None, max_scrolls=scrolls)
                # 一条都没抓到时重试没有意义:原因是登录/限流/账号改名,多翻几屏不会变出来。
                # 第一版在这里连试三轮,把同一条报错刷了三遍,反而把真正的原因埋掉了。
                if not posts or len(posts) >= want * 2:
                    break
            if not posts:
                log("   一条都没抓到(看上面的页面显示/截图)")
                continue
            posts.sort(key=lambda p: p["postedAt"], reverse=True)
            log(f"   抓到 {len(posts)} 条,从最新往回看:")
            hit = 0
            for p in posts:
                res = post_to_nas(h, [p], dry_run=True)
                if not res or res.get("error"):
                    log(f"   ❌ {res.get('error') if res else '上报失败'}")
                    break
                for pk in (res.get("parsed") or []):
                    n = len(pk.get("buys") or [])
                    flag = "🎯" if n else "  "
                    log(f"   {flag} [{p['postedAt'][:16]}] {p['text'][:34].replace(chr(10),' ')}…")
                    if n or pk.get("holds") or pk.get("exits"):
                        show_parsed(pk)
                    if n:
                        hit += 1
                if hit >= want:
                    break
                time.sleep(0.4)
            log(f"   → 找到 {hit} 条含买入信号的推文")
            time.sleep(2)
        ctx.close()
    log("")
    log("以上全是 dryRun:没入库、没推送、没建仓。")
    log("确认解析对了,就跑 3-backfill.bat 灌历史 / 2-run-poll.bat 常驻。")


def cmd_doctor():
    """自检:这个浏览器到底能不能连上 x.com。连不上就挨个试常见代理端口。

    探测**不能用 x.com/home**:那是纯 JS 渲染的单页应用,在 domcontentloaded 那一刻
    body 还是空的,网络完全正常也会被判成"页面空白"(第一版就是这么误报的)。
    改用 robots.txt —— 纯文本、不依赖 JS、有明确的 HTTP 状态码,结果没有歧义。
    """
    from playwright.sync_api import sync_playwright
    global PROXY

    def probe(proxy, headless=True):
        global PROXY
        PROXY = proxy
        with sync_playwright() as pw:
            ctx = make_context(pw, headless=headless)
            page = ctx.pages[0] if ctx.pages else ctx.new_page()
            try:
                r = page.goto("https://x.com/robots.txt", wait_until="load", timeout=15000)
                status = r.status if r else 0
                txt = (page.inner_text("body") or "").strip()
                if status == 200 and len(txt) > 30:
                    ip = "?"
                    try:
                        page.goto("https://api.ipify.org", wait_until="load", timeout=10000)
                        ip = (page.inner_text("body") or "").strip()[:40]
                    except Exception:
                        pass
                    return True, f"HTTP 200,出口 IP {ip}"
                return False, f"HTTP {status},正文 {len(txt)} 字符"
            except Exception as e:
                return False, str(e).strip().split("\n")[0][:110]
            finally:
                ctx.close()

    log("① 直连(不带代理)…")
    ok, info = probe(None)
    log(f"   {'✅' if ok else '✗'} {info}")
    if ok:
        log("")
        log("✅ 直连就通,说明 VPN 已经全局接管了,不用配代理。")
        log("   直接跑 1-install.bat 登录,然后 2-run-poll.bat 常驻。")
        return

    log("② 挨个试常见本地代理端口…")
    for p in COMMON_PROXIES:
        ok, info = probe(p)
        log(f"   {'✅' if ok else '✗'} {p}  {info}")
        if ok:
            log("")
            log("   把它固定下来,在 cmd 里执行这一条(只需一次):")
            log(f'      setx XFETCH_PROXY "{p}"')
            log("   然后关掉窗口重开,再跑 1-install.bat 登录。")
            return

    log("")
    log("❌ 全都不通。上面每一行右边就是真实原因,按它来判断:")
    log("   · ERR_PROXY_CONNECTION_FAILED / ECONNREFUSED → 那个端口上没有代理在跑")
    log("   · ERR_TIMED_OUT / Timeout                   → 代理在,但出不去(节点挂了?)")
    log("   · HTTP 403 / 404                            → 通了但被拦,多半是节点被 X 封了,换个节点")
    log("")
    log("③ 最后开一个**看得见的浏览器**,你自己瞅一眼到底什么情况…")
    probe(None, headless=False)


def scrape_handle(page, handle, want_after=None, max_scrolls=3):
    """抓一个博主主页上的推文。want_after 早于它的就不再往下翻。"""
    return scrape_url(page, f"https://x.com/{handle}", handle, want_after, max_scrolls)


def scrape_search(page, handle, since, until, max_scrolls=25):
    """按日期窗口用搜索抓。

    **回溯必须走这条路,不能靠翻主页。** 实测主页滚 80 屏也只能翻回一个月:
    X 的无限滚动到某个深度就不再加载,滚得再多也是原地打转
    (六个博主重跑一遍,抓到的条数和日期范围跟上一轮一字不差)。
    搜索按 since/until 切窗口,每个月单独一个请求,永远不需要滚很深。
    """
    q = f"from%3A{handle}%20since%3A{since}%20until%3A{until}"
    url = f"https://x.com/search?q={q}&src=typed_query&f=live"
    return scrape_url(page, url, handle, None, max_scrolls)


def scrape_url(page, url, handle, want_after=None, max_scrolls=3):
    # VPN 抖动会给出 ERR_CONNECTION_CLOSED,退避后再试一次比整轮崩掉划算
    for attempt in (1, 2):
        try:
            page.goto(url, wait_until="domcontentloaded", timeout=60000)
            break
        except Exception as e:
            if attempt == 2:
                log(f"  @{handle} 打不开:{str(e).strip().splitlines()[0][:90]}")
                return []
            log(f"  @{handle} 连接中断,8 秒后重试一次…")
            page.wait_for_timeout(8000)
    try:
        page.wait_for_selector('article[data-testid="tweet"]', timeout=25000)
    except Exception:
        # 光说"没等到推文"没法查 —— 登录墙、限流、账号改名、节点被封,现象一模一样。
        # 把页面实际显示的文字和截图都留下来,一眼就能分辨是哪种。
        try:
            body = (page.inner_text("body") or "").strip().replace("\n", " ")[:180]
        except Exception:
            body = "(读不到正文)"
        shot = os.path.join(os.path.dirname(os.path.abspath(__file__)), f"debug-{handle}.png")
        try:
            page.screenshot(path=shot, full_page=False)
        except Exception:
            shot = "(截图失败)"
        log(f"  @{handle} 没等到推文")
        log(f"     页面显示:{body}")
        log(f"     截图已存:{shot}")
        return []

    out, seen_ids, stale = {}, set(), False
    last_n, stall = 0, 0
    for i in range(max_scrolls):
        for art in page.query_selector_all('article[data-testid="tweet"]'):
            try:
                # 推文 id 从 /status/ 链接里取;同时用它确认这条确实是本人发的,
                # 转推/引用别人的会指向别人的 handle,不能算他的推荐。
                link = art.query_selector(f'a[href*="/{handle}/status/"]')
                if not link:
                    continue
                m = re.search(r"/status/(\d+)", link.get_attribute("href") or "")
                if not m:
                    continue
                tid = m.group(1)
                if tid in seen_ids:
                    continue
                seen_ids.add(tid)

                t = art.query_selector("time")
                posted = t.get_attribute("datetime") if t else None
                if not posted:
                    continue
                dt = datetime.fromisoformat(posted.replace("Z", "+00:00")).astimezone(CST)

                node = art.query_selector('div[data-testid="tweetText"]')
                text = node.inner_text() if node else ""
                if not text.strip():
                    continue
                # X 把长推文折叠成「显示更多」,时间线上只给前半截。
                # 云舒交易日记单条 1500+ 字,折叠后第二只票和全部止盈止损条款直接消失
                # —— 解析不出来不是解析器的错,是原文就没抓全。标记出来,稍后去单条页取全文。
                truncated = bool(art.query_selector('[data-testid="tweet-text-show-more-link"]')) \
                    or ("显示更多" in text) or ("Show more" in text)

                if want_after and dt < want_after:
                    # 置顶推文会以旧日期排在最前,不能一见到旧的就收工
                    stale = True
                    continue
                out[tid] = {
                    "id": tid,
                    "postedAt": dt.strftime("%Y-%m-%d %H:%M:%S"),
                    "text": text,
                    "url": f"https://x.com/{handle}/status/{tid}",
                    "_truncated": truncated,
                }
            except Exception:
                continue
        if stale and i >= 1:
            break
        # 盲滚会跑到 X 还没加载出来的地方,于是"滚了 80 屏还是那 62 条"。
        # 改成看条数有没有涨:没涨就多等一轮给它加载,连续三轮不长才认定到底了。
        if len(out) > last_n:
            last_n, stall = len(out), 0
        else:
            stall += 1
            if stall >= 3:
                break
            page.wait_for_timeout(4000)
        page.mouse.wheel(0, 3000)
        page.wait_for_timeout(2500)

    posts = list(out.values())
    expand_truncated(page, posts)
    return posts


def expand_truncated(page, posts):
    """被折叠的长推文逐条去单条页取全文。

    只对确实折叠的那些做(通常一轮就一两条),所以额外开销很小;
    但不做的话,写长文的博主(云舒交易日记)等于**每条都只解析了一半**。
    """
    todo = [p for p in posts if p.pop("_truncated", False)]
    if todo:
        log(f"     有 {len(todo)} 条被折叠,逐条取全文…")
    fails = 0
    for idx, p in enumerate(todo):
        try:
            page.goto(p["url"], wait_until="domcontentloaded", timeout=45000)
            page.wait_for_selector('article[data-testid="tweet"]', timeout=25000)
            page.wait_for_timeout(1200)
            node = page.query_selector('article[data-testid="tweet"] div[data-testid="tweetText"]')
            full = node.inner_text() if node else ""
            if len(full) > len(p["text"]):
                log(f"     展开长推文 {p['id']}:{len(p['text'])} → {len(full)} 字")
                p["text"] = full
            fails = 0
        except Exception as e:
            fails += 1
            log(f"     ⚠️ 展开 {p['id']} 失败({fails} 连败):{str(e).strip().splitlines()[0][:60]}")
            # 连续失败 = X 在限流,不是这一条有问题。硬顶着继续只会一路失败到底
            # (实测连续 8 条全 20s 超时),退避一分钟再试比闷头刷快得多。
            if fails >= 3:
                log("     ⏸ 连续 3 条展开失败,判定被限流,歇 60s 再继续…")
                page.wait_for_timeout(60000)
                fails = 0
        page.wait_for_timeout(1500)
    for p in posts:
        p.pop("_truncated", None)
    # 冷却:开了多少条单条页就歇多久(每条 1.5s,最多 25s)。
    # 不歇的话下一个博主大概率被限流,时间线一条都渲染不出来。
    if todo:
        cool = min(25, 2 + len(todo) * 1.5)
        log(f"     冷却 {cool:.0f}s 再抓下一位(避免被 X 限流)")
        page.wait_for_timeout(int(cool * 1000))


def cmd_login():
    from playwright.sync_api import sync_playwright
    with sync_playwright() as pw:
        ctx = make_context(pw, headless=False)
        page = ctx.pages[0] if ctx.pages else ctx.new_page()
        page.goto("https://x.com/login")
        print("\n请在弹出的浏览器里登录 X。登录完成后回到这里按回车。\n")
        input()
        ctx.close()
    log("✅ 登录态已存到 " + SESSION_DIR)


def run_round(page, seen):
    total_new = 0
    for h in HANDLES:
        try:
            posts = scrape_handle(page, h, want_after=datetime.now(CST) - timedelta(days=3))
        except Exception as e:
            log(f"  @{h} 抓取异常:{e}")
            continue
        fresh = [p for p in posts if p["id"] not in seen]
        if not fresh:
            continue
        res = post_to_nas(h, fresh)
        for p in fresh:
            seen.add(p["id"])
        if res and not res.get("error"):
            n = res.get("new", 0)
            total_new += n
            upd = res.get("updated", 0)
            buys = sum(len(x.get("buys") or []) for x in (res.get("parsed") or []))
            review = sum(1 for x in (res.get("parsed") or []) if x.get("needsReview"))
            log(f"  @{h} 上报 {len(fresh)} 条 → 新增 {n}" + (f",补全 {upd}" if upd else "") +
                f",买入信号 {buys} 只" + (f",⚠️{review} 条待人工复核" if review else ""))
        elif res:
            log(f"  @{h} 服务端拒收:{res['error']}")
        time.sleep(2)   # 别把六个主页连着刷,X 对高频访问很敏感
    save_seen(seen)
    return total_new


def cmd_poll(once):
    from playwright.sync_api import sync_playwright
    seen = load_seen()
    with sync_playwright() as pw:
        ctx = make_context(pw, headless=HEADLESS)
        page = ctx.pages[0] if ctx.pages else ctx.new_page()
        if not check_login(page):
            ctx.close()
            return
        while True:
            log(f"—— 开始一轮({len(HANDLES)} 个博主)——")
            try:
                n = run_round(page, seen)
                log(f"本轮新增 {n} 条")
            except Exception as e:
                log(f"❌ 本轮异常:{e}")
            if once:
                break
            time.sleep(POLL_SECONDS)
        ctx.close()


def post_batches(handle, posts, batch=8):
    """分批上报,**失败要重试并如实计数**。

    上一版这里写的是 `if res and res.get("error"): break` —— res 为 None(两条路都失败)时
    直接进入下一批,**整批数据就这么静默丢了**,日志上只有一行红字,谁也不知道少了多少。
    抓了两个小时的东西不能这么丢。
    """
    ok, lost = 0, 0
    for i in range(0, len(posts), batch):
        chunk = posts[i:i + batch]
        for attempt in (1, 2, 3):
            res = post_to_nas(handle, chunk)
            if res and not res.get("error"):
                ok += len(chunk)
                upd = res.get("updated", 0)
                if res.get("new") or upd:
                    log(f"    上报 +{res.get('new', 0)} 新" + (f" / 补全 {upd}" if upd else ""))
                break
            if res and res.get("error"):
                log(f"    服务端拒收:{res['error']}")
                return ok, lost + len(posts) - i
            if attempt < 3:
                log(f"    第 {attempt} 次上报失败,{attempt * 15}s 后重试…")
                time.sleep(attempt * 15)
        else:
            lost += len(chunk)
            log(f"    ⚠️ 这 {len(chunk)} 条三次都没传上去,已放弃(重跑本脚本可补)")
        time.sleep(2)
    return ok, lost


def month_windows(after_date, today):
    """把 [after_date, today] 切成整月窗口,从近到远。"""
    wins, end = [], today
    while end > after_date:
        first = end.replace(day=1)
        start = max(after_date, first if end.day != 1 else (first - timedelta(days=1)).replace(day=1))
        if start >= end:
            start = end - timedelta(days=30)
        wins.append((start, end))
        end = start - timedelta(days=1)
        if len(wins) > 24:
            break
    return wins


def cmd_backfill(days, only_handle):
    """按月切片回溯。

    **不能靠翻主页**:实测滚 80 屏也只翻得回一个月,X 的无限滚动到某个深度就不再加载,
    重跑一遍抓到的条数和日期范围一字不差(62/77/33,和库里完全一样)。
    改用搜索的 since/until 按月开窗,每个窗口都是独立请求,永远不需要滚很深。
    """
    from playwright.sync_api import sync_playwright
    seen = load_seen()
    today = datetime.now(CST).date()
    after = today - timedelta(days=days)
    targets = [only_handle] if only_handle else HANDLES
    with sync_playwright() as pw:
        ctx = make_context(pw, headless=HEADLESS)
        page = ctx.pages[0] if ctx.pages else ctx.new_page()
        if not check_login(page):
            ctx.close()
            return
        for h in targets:
            log(f"回溯 @{h} 近 {days} 天(按月切片)…")
            posts, ids = [], set()
            for start, end in month_windows(after, today):
                try:
                    got = scrape_search(page, h, start.isoformat(), (end + timedelta(days=1)).isoformat())
                except Exception as e:
                    log(f"    {start} ~ {end}  异常:{str(e).strip().splitlines()[0][:70]}")
                    page.wait_for_timeout(8000)
                    continue
                fresh = [x for x in got if x["id"] not in ids]
                for x in fresh:
                    ids.add(x["id"])
                posts.extend(fresh)
                log(f"    {start} ~ {end}  {len(fresh):>3} 条(累计 {len(posts)})")
                page.wait_for_timeout(6000)   # 窗口之间歇一下,别把搜索也搞限流
            log(f"  合计抓到 {len(posts)} 条,上报中…")
            ok, lost = post_batches(h, posts)
            log(f"  @{h} 上报完成:成功 {ok} 条" + (f",⚠️丢失 {lost} 条" if lost else ""))
            for x in posts:
                seen.add(x["id"])
            save_seen(seen)
            time.sleep(5)
        ctx.close()


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("login")
    sub.add_parser("doctor")
    tt = sub.add_parser("test")
    tt.add_argument("--handle", default="", help="只测某一个博主")
    tt.add_argument("--want", type=int, default=2, help="每人找几条含买入信号的就停")
    p = sub.add_parser("poll")
    p.add_argument("--once", action="store_true", help="只跑一轮(配合任务计划程序)")
    b = sub.add_parser("backfill")
    b.add_argument("--days", type=int, default=180)
    b.add_argument("--handle", default="")
    args = ap.parse_args()

    if args.cmd not in ("login", "doctor") and not NAS_TOKEN:
        print("❌ 没设 JCP_TOKEN 环境变量,上报会被服务端拒绝。先运行 setup.bat 或手动 set JCP_TOKEN=…")
        sys.exit(1)

    log(f"xfetch {VERSION}" + (f" · 代理 {PROXY}" if PROXY else "") + ("" if HEADLESS else " · 有头模式"))
    if args.cmd == "doctor":
        cmd_doctor()
    elif args.cmd == "test":
        cmd_test(args.handle.lstrip("@"), args.want)
    elif args.cmd == "login":
        cmd_login()
    elif args.cmd == "poll":
        cmd_poll(args.once)
    else:
        cmd_backfill(args.days, args.handle.lstrip("@"))


if __name__ == "__main__":
    main()
