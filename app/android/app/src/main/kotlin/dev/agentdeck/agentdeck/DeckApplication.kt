package dev.agentdeck.agentdeck

import android.app.Application
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.engine.dart.DartExecutor

class DeckApplication : Application() {
    val engine: FlutterEngine by lazy {
        FlutterEngine(this).also {
            DownloadBridge.attach(this, it)
            UpdateBridge.attach(this, it)
            OutboxBridge.attach(this, it)
            it.dartExecutor.executeDartEntrypoint(DartExecutor.DartEntrypoint.createDefault())
        }
    }
}
