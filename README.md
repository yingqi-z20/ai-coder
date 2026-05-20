# AI Coder - THUCS

AI Coder - THUCS 是一个面向数字逻辑实验的 VS Code 扩展，用聊天界面把 AI 辅助、Vivado Tcl 控制台和常用实验流程串在一起。

## 功能

- 在 VS Code 侧边栏中打开 AI Coder 聊天视图。
- 根据实验需求生成 RTL、Testbench、XDC 等文件内容。
- 一键创建/打开 Vivado 工程、设置顶层、导入约束、运行仿真、综合、实现和生成 bitstream。
- 将 Tcl Console 最近日志粘贴到聊天框，便于让 AI 分析报错。
- AI 回复中的 `<__FILE_WRITE__>` / `<__FILE_APPEND__>` 指令会写入当前工作区文件。
- AI 回复中的 `<__VIVADO_CMD__>` 指令会发送到后端 Vivado Tcl 会话执行。

## 使用前准备

1. 安装扩展包 `ai-coder-thucs-*.vsix`。
2. 打开一个实验工作区文件夹。
3. 确认后端服务可访问，并配置扩展运行环境变量：
   - `DOMAIN`：后端域名，默认 `ai-coder.thucs.cn`
   - `API_KEY`：访问后端 API 的密钥
4. 后端运行环境需要可执行 Vivado，默认路径为 `/tools/Xilinx/Vivado/2024.2/bin/vivado`。

## 后端配置

后端必须配置：

- `API_KEY`：扩展访问后端时使用的鉴权密钥。

AI 模型相关配置：

- `MODEL_API_KEY` 或 `OPENAI_API_KEY`：模型服务密钥。
- `MODEL_BASE_URL`：兼容 OpenAI API 的模型服务地址，可选。
- `MODEL_NAME`：模型名称，默认 `qwen3.6-plus`。

Docker Compose 示例见 [docker-compose.yml](docker-compose.yml)。

## 开发

安装依赖：

```bash
npm install
```

编译扩展：

```bash
npm run compile
```

检查前端扩展代码：

```bash
npm run lint
```

运行后端测试：

```bash
cd Backend
go test ./...
```

## 常见问题

- 如果侧边栏能打开但聊天失败，先检查 `DOMAIN` 和 `API_KEY`。
- 如果 AI 回复“后端未配置模型密钥”，检查 `MODEL_API_KEY` 或 `OPENAI_API_KEY`。
- 如果 Vivado 操作没有输出，检查后端机器是否存在 Vivado 可执行文件，以及当前工作区路径是否可访问。
- 如果文件没有写入，确认 VS Code 当前打开的是工作区文件夹，而不是单个文件。
