# Pandavan Homepage Probe

轻量、零依赖的 Padavan / 嵌入式 Linux 路由器状态探针与安全反代服务。专为 **[Homepage](https://gethomepage.dev/)**、Dashy、Homarr 等内网仪表盘设计。

---

## 为什么需要这个项目？

1. **Padavan 固件只读无 `rpcd`/`ubus`**：
   - OpenWrt 官方拥有完善的 RPC 生态，但全网海量基于联发科 MT7621 / 华硕方案（如红米 CR660x、AC2100、斐讯 K2P 等）刷入的 **Padavan（老毛子）固件** 采用只读 SquashFS，无法安装体积庞大的 Prometheus Node Exporter 或 Python 运行环境。
2. **纯二进制架构，极致轻量**：
   - 采用标准 Go 编写，单文件静态编译，无额外第三方依赖，不给只读路由器本身增加任何内存与存储负担。
   - 可以在旁路由、家庭 NAS（如 fnOS / 群晖 / TrueNAS）上作为单文件独立运行，通过 SSH 免密只读抓取路由器底层 `/proc` 数据，兼顾安全与稳定。
3. **二合一功能（探针 + 反代）**：
   - `/api/status.json`：向 Homepage 等前端吐出标准 JSON 指标；
   - `/`：自动反代路由器原本的 Web 管理页面，配合 Cloudflare Tunnel 等工具可实现免改端口安全远程访问路由器后台。

---

## 核心特性

- **指标采集**：
  - 运行时间（Uptime Days / Seconds）
  - CPU 负载（1分钟 / 5分钟平均负载）
  - 内存实际占用与使用率（RAM Used / Total / Percent）
  - 在线状态探测（Online / Offline 自动熔断）
- **性能开销极低**：
  - 内置内存级 LRU 缓存（默认 10 秒），杜绝频繁请求路由器 CPU；
  - 编译后二进制仅 ~7MB，内存占用仅 10MB。
- **开箱即用**：
  - 标准化适配 Homepage 的 `customapi` 组件，无需写任何前端代码。

---

## 快速开始

### 1. 下载或编译

#### 本地编译（需 Go 1.20+）：
```bash
# 编译当前系统架构
go build -ldflags="-s -w" -o pandavan-probe main.go

# 交叉编译至 Linux x86_64 (如各类 NAS / Linux 宿主机)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o pandavan-probe-linux-amd64 main.go

# 交叉编译至 ARM64 (如树莓派 / 软路由)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o pandavan-probe-linux-arm64 main.go
```

---

### 2. 运行探针

在与路由器同一局域网的主机（如 NAS、小主机）上运行：

```bash
./pandavan-probe \
  -listen ":18080" \
  -router "192.168.1.1" \
  -user "admin" \
  -key "/path/to/id_ed25519" \
  -proxy "http://192.168.1.1:80" \
  -cache 10
```

#### 参数说明：
| 参数 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `-listen` | `:18080` | 探针 HTTP 监听端口 |
| `-router` | `192.168.1.1` | 目标 Padavan 路由器 IP 或主机名 |
| `-user` | `admin` | 路由器 SSH 登录用户名 |
| `-key` | `""` | 路由器 SSH 登录私钥路径（留空则使用默认 `~/.ssh/id_rsa` / `id_ed25519`） |
| `-port` | `22` | 路由器 SSH 端口 |
| `-proxy` | `http://192.168.1.1:80` | 路由器原始 Web 管理界面地址（用于反向代理） |
| `-cache` | `10` | 硬件状态数据内存缓存秒数 |

---

### 3. 配置 systemd 开机自启（推荐）

创建服务文件 `/etc/systemd/system/pandavan-probe.service`：

```ini
[Unit]
Description=Pandavan Homepage Probe & Reverse Proxy
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/pandavan-probe -listen :18080 -router 192.168.1.1 -user admin -key /root/.ssh/id_ed25519
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

启动并设置开机自启：
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now pandavan-probe.service
```

---

## Homepage 仪表盘配置接入

在 Homepage 的 `services.yaml` 中添加以下配置即可开箱即用显示卡片状态：

```yaml
- 核心基础设施 (Core Services):
    - 路由器后台 (Padavan):
        icon: router.png
        href: https://router.yourdomain.com/  # 你的公网或局域网访问地址
        description: 红米 CR660x 主网关
        ping: http://192.168.1.1:80
        widget:
          type: customapi
          url: http://10.2.2.1:18080/api/status.json  # 探针提供的状态接口
          refreshInterval: 10000
          mappings:
            - field: uptime
              label: 运行时间
              format: text
            - field: load
              label: 系统负载
              format: text
            - field: memory
              label: 内存占用
              format: text
```

---

## API 规范

### `GET /api/status.json`

返回当前路由器的硬件实时状态：

```json
{
  "uptime": "203天",
  "uptime_days": 203,
  "load": "0.08",
  "load_1": 0.08,
  "load_5": 0.02,
  "memory": "66.5% (160MB)",
  "mem_used_mb": 160,
  "mem_total_mb": 240,
  "mem_percent": 66.5,
  "status": "Online",
  "last_check": "2026-11-08 10:45:12"
}
```

---

## 开源协议

MIT License.
