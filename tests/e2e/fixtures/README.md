# 文件预览样例

这些样例只包含本仓库生成的图形、纯音或静音，不含外部作品和私人文件。JSON 中保存 Base64 字节，浏览器用例把它们写入各自的隔离工作区；运行测试不需要安装媒体编码器。

- `file-images.json`：Pillow 生成的色块与几何图形，覆盖八种目标图片扩展名；GIF、WebP 含两帧，ICO 含两种尺寸。尺寸和帧数在生成后重新打开核对。
- `file-audio.json`：一秒 440 Hz PCM 纯音，经系统 AudioToolbox 转为 AAC ADTS、FLAC 和 MPEG-4 AAC；M4A/MP4A 共享容器字节。编码器填充会使 AAC 容器时长略大于一秒。WAV 由测试直接构造。
- `file-video.json`：Canvas 色块通过 Chromium MediaRecorder 录制为 H.264 MP4，再由系统媒体工具重封装以补齐有限时长；无音轨。用例同时验证原始尺寸、实际播放、暂停、定位和控件可用宽度。

这些样例只证明所选容器与编码，不代表所有平台、所有编码变体均可播放。其余编码与原生平台的验证需另行覆盖。

`workspace-media-reference.spec.ts` 可通过 `COBOT_E2E_EXTERNAL_MEDIA_DIR` 指定外部样例目录。目录需包含样例及 `download-manifest.json`；用例在复制到测试工作区前核对 SHA-256，不自动下载。参考样例来自 [Chromium 媒体测试库](https://github.com/chromium/chromium/tree/main/media/test/data)，来源提交与许可证文件随本地样例记录。平台不支持时的明确错误，与实际播放成功分别记入结果，不能混算。
