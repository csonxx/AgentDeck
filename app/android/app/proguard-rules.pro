# MethodChannel entry points and their worker closures contain platform APK
# validation calls. Preserve this small bridge as one unit instead of expanding
# it through R8's whole-program optimizer; the rest of release shrinking stays on.
-keep class dev.agentdeck.agentdeck.UpdateBridge** { *; }
