# 旧 Office 与文本预览 / Legacy Office and text previews

旧 Word/Excel 解析基于微软公开的 [MS-DOC](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-doc/ccd7b486-7881-484c-a137-51170af7cc22) 与 [MS-XLS](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-xls/cd03cb5f-ca02-4934-a391-bb674cb8aa06) 格式。支持 Word 97–2003 的正文 piece table，以及 Excel 97–2003 / BIFF8 的工作表、共享字符串（包括 CONTINUE 压缩切换）、数值、布尔/错误值和公式缓存。

The parsers follow Microsoft's public MS-DOC and MS-XLS formats. Supported content includes the Word 97–2003 body piece table and Excel 97–2003 / BIFF8 sheets, shared strings (including CONTINUE compression changes), numbers, booleans/errors and cached formula results.

CFB 容器使用 Apache-2.0 许可的 `github.com/richardlehane/mscfb v1.0.9`，其属性支持依赖为 Apache-2.0 许可的 `msoleps v1.0.3`；字符集使用 BSD-3-Clause 许可的 `golang.org/x/text v0.30.0`。二进制正文/BIFF 解析代码在本项目中按格式规范实现。运行时无需 Office/LibreOffice，也不提取或执行宏、公式、外部链接。

The CFB container uses Apache-2.0-licensed `github.com/richardlehane/mscfb v1.0.9` with Apache-2.0-licensed `msoleps v1.0.3`; character decoding uses BSD-3-Clause-licensed `golang.org/x/text v0.30.0`. Body/BIFF parsing is implemented here against the specifications. Runtime requires no Office/LibreOffice installation and neither extracts nor executes macros, formulas or external links.

内容转换仅在 S-LOCAL 内存执行，通过现有加密 WebSocket 返回 `application/vnd.agentdeck.office+json`，不保存明文转换产物。上传原件仍为原字节，供 agent 读取；预览 RPC `upload_preview` 沿用手机所有权、会话分支和有效期检查。手机后台 isolate 另行校验 JSON 类型和大小。

Conversion occurs only in S-LOCAL memory, returning `application/vnd.agentdeck.office+json` over the existing encrypted WebSocket. No plaintext converted file is persisted. Uploaded originals remain byte-identical for agents; `upload_preview` retains owner, session ancestry and expiry checks. A phone isolate validates the resulting JSON types and bounds.

限制 / Limits:

- 输入最多 8 MiB；容器读取次数、扇区数量、流大小和偏移均受限。 / Inputs: 8 MiB, with bounded container reads, sector counts, stream sizes and offsets.
- DOC：正文最多 500,000 UTF-16 码元、约 1 MiB UTF-8、10,000 段；有截断提示。 / DOC: 500,000 UTF-16 units, roughly 1 MiB of UTF-8 text and 10,000 paragraphs, with a truncation notice.
- XLS：50 工作表，每表 2,000 行/100 列，单格 4,096 码元；全局最多 50,000 个存储单元格、200,000 个显示网格位置及约 4 MiB 值预算。 / XLS: 50 sheets, 2,000 rows/100 columns per sheet, 4,096 units per value; global budgets of 50,000 stored cells, 200,000 displayed grid positions and roughly 4 MiB of values.
- 只显示正文与内容，不重建页眉页脚、脚注、形状、图表或日期/数值格式。密码保护和损坏文件显示错误，更早的 Word/BIFF 版本不支持。 / Content previews do not reconstruct headers/footers, footnotes, shapes, charts or date/number formatting. Password-protected/damaged files report errors; earlier Word/BIFF versions are unsupported.
- 文本：UTF-8、带 BOM 的 UTF-16；明确文本扩展名可解码 GB18030/GBK，采用回编码校验并拒绝二进制控制符。GBK/Big5 无通用自动判别，Big5 和其他未支持编码请先转 UTF-8。脱敏后仅传前 128 KiB，客户端显示截断提示，原始附件不受影响。 / Text: UTF-8, BOM-marked UTF-16 and GB18030/GBK for explicit text extensions, with roundtrip validation and rejection of binary control characters. GBK and Big5 cannot be universally distinguished; convert Big5/other unsupported encodings to UTF-8 first. Only a redacted 128 KiB prefix is sent with a truncation notice; original uploads are unaffected.

测试使用独立的 textutil/xlwt 生成的合成二进制文件，参见 `backend/internal/document/testdata/README.md`；测试生成器不属于运行时依赖。 / Tests use independently generated textutil/xlwt synthetic binaries; see `backend/internal/document/testdata/README.md`. Fixture generators are not runtime dependencies.

许可副本 / License copies: [Apache 2.0](licenses/Apache-2.0.txt), [Go BSD 3-Clause](licenses/Go-BSD-3-Clause.txt).
