# Client speech model / 客户端语音模型

Runtime: [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx), Apache-2.0, Copyright Xiaomi Corporation and sherpa-onnx contributors. Flutter uses the sherpa_onnx package; its license is included in Flutter's license registry.

模型 / Model: [csukuangfj/k2fsa-zipformer-bilingual-zh-en-t](https://huggingface.co/csukuangfj/k2fsa-zipformer-bilingual-zh-en-t), declared Apache-2.0 by the upstream model card. Revision: `e2382758de9a0219b4efe682b95af30b399db3b8`. The public model is supplied by the upstream contributor; AgentDeck does not claim authorship. / 上游 model card 声明 Apache-2.0；模型由上游提供，AgentDeck 不主张其作者身份。

AgentDeck selects the int8 encoder/joiner and float32 decoder exports in `exp/32` and `data/lang_char_bpe/tokens.txt`. No model modifications. Source paths and SHA-256 hashes are pinned in `app/assets/asr/manifest.json`. Model binaries are excluded from Git; `python3 scripts/setup_client_voice.py` retrieves and verifies them before building. / 使用 exp/32 的 int8 encoder/joiner、float32 decoder 和词表，不修改模型；源文件和哈希在 manifest 中固定，模型二进制不入 Git，构建前用脚本获取并校验。

[Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0)


整句校正 / Utterance correction: **SenseVoiceSmall**, Alibaba Group / FunAudioLLM, under [FunASR Model Open Source License Agreement 1.1](https://github.com/modelscope/FunASR/blob/main/MODEL_LICENSE), **not Apache-2.0**. Original: [FunAudioLLM/SenseVoiceSmall](https://huggingface.co/FunAudioLLM/SenseVoiceSmall). ONNX conversion by sherpa-onnx / Fangjun Kuang: [converted model](https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17), revision `2365baeacb507f821a0c8120fcee3d484dba7a07`. The int8 weights and tokens are unmodified. Source/name/authorship notices and the full model agreement are packaged as `REFINER_NOTICE` and `REFINER_LICENSE`; the latter is also registered in Flutter’s licenses. SHA-256 and source revision are pinned in the manifest. / 使用未修改的 int8 权重和词表，保留出处、模型名称、作者及完整模型协议，模型来源版本及哈希固定在 manifest，协议同时注册到 Flutter 许可证页。
