# fmp4audit — 分片 MP4 音频时间线审计服务

音频归档平台的入库前置校验：在接收分片 MP4（fMP4 / ISO BMFF）前，核对初始化片段与媒体片段的解码时间线，避免播放器自动容错掩盖**音频重叠、空洞或载荷错配**。

服务从零依赖的 Go 标准库实现，逐个解析盒（box）结构，从轨道时标、`trex` 默认采样参数以及每个片段的 `tfhd` / `tfdt` / `trun` 还原实际采样时长与载荷范围，并校验：

- 单一音轨（`hdlr` = `soun`）且存在 `mvex`/`trex`；
- 轨道一致（`tfhd.track_ID` 与 init 轨道一致）；
- 片段序号（`mfhd.sequence_number`）严格递增；
- 采样时长/尺寸可经 `trun → tfhd → trex` 链完整继承；
- 媒体字节完整（采样尺寸之和精确等于 `mdat` 载荷）；
- 相邻解码区间**精确衔接**（前一段结束刻度 == 后一段 `tfdt` 起始刻度）。

## 运行

```bash
# 启动 API（宿主机端口可配置，默认 8080）
API_PORT=9000 docker compose up --build api

# 一次性验证：等待 api 健康后执行 构建检查 + 单元测试 + HTTP 冒烟（连续与断裂时间线）
# 以退出码报告结果（0 = 全部通过）
docker compose up --build --exit-code-from verify
```

健康检查：`GET /healthz`（Compose 已配置 healthcheck，`verify` 通过 `depends_on: service_healthy` 等待）。

## API

### `POST /api/fmp4/audit`

`multipart/form-data`，**按顺序**上传：第 1 个 part 为初始化片段，其后 1–32 个 part 为媒体片段；所有 part 合计不超过 16 MiB。

```bash
curl -s http://localhost:8080/api/fmp4/audit \
  -F "init=@init.mp4" -F "seg=@seg1.m4s" -F "seg=@seg2.m4s"
```

**成功响应 `200`**（按提交顺序给出各片段序号、起止解码刻度、采样数及整段时长）：

```json
{
  "ok": true,
  "timescale": 48000,
  "track_id": 1,
  "segment_count": 2,
  "total_duration": 3072,
  "segments": [
    {"index": 0, "sequence_number": 1, "start": 0,    "end": 2048, "samples": 2, "duration": 2048},
    {"index": 1, "sequence_number": 2, "start": 2048, "end": 3072, "samples": 1, "duration": 1024}
  ]
}
```

**失败响应 `4xx`**：返回出错的片段索引（`segment_index`，init 片段为 `-1`）与稳定错误码：

```json
{
  "ok": false,
  "error": {
    "code": "TIMELINE_GAP",
    "segment_index": 1,
    "message": "media: decode start 2144 but previous segment ends at 2048 (gap of 96 ticks)"
  }
}
```

### 错误码

| HTTP | code | 含义 |
|---|---|---|
| 400 | `BAD_MULTIPART` | 非 multipart 请求或 multipart 体损坏 |
| 400 | `BAD_PART_COUNT` | part 数量不符（须为 1 个 init + 1–32 个媒体片段） |
| 400 | `EMPTY_PART` | 存在空 part |
| 413 | `TOO_LARGE` | 合计载荷超过 16 MiB |
| 422 | `BOX_STRUCTURE` | 盒结构非法（截断、尺寸越界、缺失必需盒、init/媒体位置错放等） |
| 422 | `NOT_SINGLE_AUDIO_TRACK` | 非单一音轨（轨道数 ≠ 1 或 handler 非 `soun`） |
| 422 | `NO_MVEX` | init 缺少 `mvex`（非分片 MP4） |
| 422 | `NO_TREX` | `mvex` 中无该轨道的 `trex` |
| 422 | `TRACK_MISMATCH` | `tfhd.track_ID` 与 init 轨道不一致 |
| 422 | `SEQUENCE_NOT_INCREASING` | `mfhd` 序号未严格递增 |
| 422 | `NO_TFDT` | `traf` 缺少 `tfdt`（baseMediaDecodeTime） |
| 422 | `PARAM_INHERIT` | 采样时长/尺寸在 trun、tfhd、trex 中均无法解析 |
| 422 | `PAYLOAD_MISMATCH` | 采样尺寸之和与 `mdat` 载荷字节数不符 |
| 422 | `TIMELINE_GAP` | 相邻解码区间存在空洞 |
| 422 | `TIMELINE_OVERLAP` | 相邻解码区间存在重叠 |

归档人员可依据 `code` 与 `segment_index` 拒收存在时间线缺口或重叠的音频。

## 本地开发

```bash
go test ./...          # 单元测试
go run ./cmd/server    # 启动 API（PORT 环境变量可改端口，默认 8080）
go run ./cmd/smoke     # 对运行中的 API 做 HTTP 冒烟
```

## 结构

```
cmd/server     API 服务入口
cmd/smoke      HTTP 冒烟检查器（连续/断裂时间线等用例）
internal/fmp4  ISO BMFF 盒解析与审计逻辑（错误码定义于此）
internal/server HTTP 层（multipart、限长、JSON 响应）
internal/gen   测试夹具生成器（构造 init/媒体片段）
scripts/verify.sh  verify 服务入口：构建检查 → 单元测试 → 健康等待 → HTTP 冒烟
```
