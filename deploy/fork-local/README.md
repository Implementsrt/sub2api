# Windows 本机更新流程

本 fork 的本机入口为根目录 `start-local.cmd` / `restart-local.cmd`。脚本版本受 Git 管理；`local-runtime/` 中的程序、凭证、配置、备份和日志全部忽略，不提交。

桌面入口：执行 `powershell -NoProfile -ExecutionPolicy Bypass -File deploy/fork-local/create-shortcut.ps1`，创建 `Sub2API Restart` 快捷方式，直接指向本仓库的 `restart-local.cmd`，工作目录固定为仓库目录。Windows 不支持单独设置 cmd 文件图标，因此在快捷方式上使用 `sub2api.ico`；图标由原项目 `assets/logo.svg` 转换，包含 16/24/32/48/64/128/256 多种尺寸。已存在的同名快捷方式先备份到忽略的 `local-runtime/backups/shortcuts/`。只创建快捷方式，不启动或重启服务。

## 完成功能后

1. 运行修改对应的测试与检查。
2. 在仓库根目录执行 `powershell -NoProfile -ExecutionPolicy Bypass -File deploy/fork-local/build.ps1`。可用 `-Version 0.2.21-personal.6` 指定版本；默认使用源码版本及当前时间。Go 不在 PATH 时可用 `-GoExe` 指定。
3. 构建脚本先运行语言包检查、TypeScript、Vite 和 Go embed 构建。全部成功才发布 `local-runtime/latest-build.json`。新版程序保存在独立的 `releases/<ID>/sub2api.exe`；构建失败不覆盖原待启用版本。前端依赖必须事先按仓库锁文件安装。
4. 提交并推送源码到 `origin` 当前分支。遵循根目录 `AGENTS.md`；不得提交私密运行文件。

## 用户重启时

双击 `restart-local.cmd`，或者在 PowerShell 中执行 `./restart-local.cmd` 以保留窗口输出。存在新构建时：

- 使用独占锁阻止并发发布/重启，检查发布路径、SHA-256、程序版本和现有服务 PID；拒绝停止无关进程。
- 先保存旧 exe 和私密 data 配置目录，再停止本机服务，对最终数据库状态执行 `pg_dump -Fc` 并使用 `pg_restore --list` 校验归档。备份失败不替换程序，恢复启动旧服务。
- 将验证后的程序复制为 `local-runtime/sub2api.exe` 并启动。新程序自动执行其内嵌迁移；脚本核对健康状态、实际版本及迁移记录。
- 新程序启动前失败，或没有待执行迁移时启动失败，可恢复旧程序。新程序可能已执行迁移时，不自动恢复旧程序或数据库；停止失败服务，保留备份和日志供恢复。数据库不会被脚本自动覆盖。

没有新构建时，只重启已安装程序。`start-local.cmd` 保留普通启动语义，不切换版本。现有 `local-runtime/start.ps1 -RestartApp` 兼容入口转发到新版脚本。脚本不重启 Codex。

当前本机工具默认位于 `D:\software\Python\Python313\python.exe` 和 `D:\software\PostgreSQL\18\bin`，Redis 路径沿用现有安装。迁移到另一台机器时需传入 `restart.ps1 -PythonExe ... -PgBin ...`，并调整 `start.ps1` 中的 Redis 路径。数据库 helper 只接受本机安装器生成的 database 标量配置；凭证只传入 PostgreSQL 子进程环境，不打印原始诊断。该流程面向本机单服务运行；不阻止外部程序直接写同一个数据库。

## 验证，不重启

`powershell -NoProfile -ExecutionPolicy Bypass -File deploy/fork-local/restart.ps1 -Check` 只检查当前程序、发布产物及待执行迁移，不停止服务、不运行数据库迁移。

发布脚本测试：从 `local-runtime/latest-build.json` 取得当前发布程序路径和版本，传给 `test-release.ps1 -Candidate ... -Version ...`。不要依赖可能已清理的旧版散落 exe。

重启流程测试：使用当前发布路径及版本运行 `test-restart.ps1 -Candidate ... -Version ... -OldExecutable local-runtime/sub2api.exe`。使用模拟进程、网络和数据库验证升级成功、备份失败、无迁移启动失败回退、有迁移启动失败停止四种情况，不启动任何服务。

数据库 helper 测试：`python deploy/fork-local/test_database.py`。测试数据库命令使用模拟环境，不改动真实数据库。测试夹具保留于忽略的 `local-runtime/release-checks/`。上述测试命令中的候选程序及版本需随实际构建调整。

## 构建产物保留策略

成功发布后自动运行保留策略，默认保留 3 份发布产物（包含最新待启用版本及与已安装程序相同的版本，其他名额保留最近发布）。任何正在运行的程序都受保护；受保护版本超过 3 份时优先保留。多余发布目录、中间构建副本和旧版 `sub2api-custom.exe` / `sub2api-personal.N.exe` 移入 Windows 回收站，可从系统回收站恢复；不再生成一份清理归档。回收站占用由 Windows 管理，永久释放磁盘空间需清空回收站。

构建持有独占 `build.lock`，发布与重启共享 `update.lock`。直接发布只回收本次已发布的中间副本；完整构建成功后仍持有构建锁，可一并回收历史中间副本。单独清理也按相同顺序取得两把锁。清理不修改待启用清单、不切换程序，异常或被占用文件保留，不影响已经成功的发布。只处理命名符合规则的产物，拒绝越界路径及符号链接/目录联接；遇到未知内容保留。不会自动清理数据库、配置、日志、升级备份、原始安装包及测试夹具。

预览：`powershell -NoProfile -ExecutionPolicy Bypass -File deploy/fork-local/cleanup.ps1 -DryRun`；执行时去掉 `-DryRun`，可用 `-KeepReleases 3` 调整（2–10）。清理策略测试：`powershell -NoProfile -ExecutionPolicy Bypass -File deploy/fork-local/test-retention.ps1`，仅移动合成测试文件，不操作真实版本或系统回收站。仅调整维护脚本时不重新编译或递增应用版本。
