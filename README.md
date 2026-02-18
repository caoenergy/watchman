
# Watchman - Linux 文件系统监控工具

## 系统要求

- Linux 内核版本 ≥ 5.9
- 需要 CAP_SYS_ADMIN 和 CAP_DAC_READ_SEARCH 权限

## 安装

1. 确保 Go 环境已安装 (Go 1.25.7)
2. 克隆项目:
   ```bash
   git clone https://github.com/caoenergy/watchman.git
   cd watchman
   ```
3. 构建项目:
   ```bash
   cd watchman
   make build
   ```

## 权限设置

由于需要访问系统底层接口，需要为程序设置相应权限:

```bash
sudo setcap cap_sys_admin,cap_dac_read_search+ep watchman
```
