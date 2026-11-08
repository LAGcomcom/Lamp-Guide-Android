# 来源与第三方组件

Lamp Guide Android 从 [Lamp Guide](https://github.com/LAGcomcom/Lamp-Guide) 的安卓移植工程独立整理，沿用其 Go 网关与版权声明。

| 来源 | 使用范围 | 许可位置 |
| --- | --- | --- |
| [LAGcomcom/zen-gate](https://github.com/LAGcomcom/zen-gate) / [Lamp Guide](https://github.com/LAGcomcom/Lamp-Guide) | 网关、渠道、账号、模型与管理桥 | [LICENSE](LICENSE) |
| [dsh-our-free-model](https://github.com/Ebony-Vinyl/dsh-our-free-model) | 部分渠道协议与实现参考 | [独立声明](licenses/dsh-our-free-model.txt) |
| [iJetLi/dsh-providers-hub](https://github.com/iJetLi/dsh-providers-hub) | 渠道、额度、目录、签到与聚合实现参考 | [独立声明](licenses/dsh-providers-hub.txt) |
| [qodercn-gateway](https://github.com/Morpheus799/qodercn-gateway) | 内嵌 QoderCN 网关 | [原组件完整 LICENSE](gateway/internal/zen/qoderhub/LICENSE) |
| Go、x/mobile、x/net、x/sys、go-qrcode、yaml.v3 等 | Go 运行时与依赖 | 版本见 `gateway/go.mod` / `gateway/go.sum`，遵守各组件许可 |
| AndroidX / Jetpack Compose、Kotlin、Gradle、AGP | 安卓界面与构建 | 遵守对应项目许可，版本见 Gradle 文件 |
| @resvg/resvg-js | SVG 图标构建，非应用运行依赖 | `package-lock.json` 固定版本，MIT / Apache-2.0 |

QoderCN LICENSE 中对 `coolxll/lingma-ipc-proxy` 来源和授权范围的说明原样保留，根目录许可不扩大第三方权利。

提灯 SVG 由本项目编写，适用根目录许可证。第三方模型、渠道和产品名称与商标属于各自所有者。
