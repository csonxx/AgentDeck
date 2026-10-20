package dev.agentdeck.agentdeck

import android.app.Service
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

object OutboxBridge {
    private var channel: MethodChannel? = null
    fun attach(context: Context, engine: FlutterEngine) {
        channel = MethodChannel(engine.dartExecutor.binaryMessenger, "dev.agentdeck/outbox").also { c ->
            c.setMethodCallHandler { call, result ->
                if (call.method != "background") { result.notImplemented(); return@setMethodCallHandler }
                try {
                    val intent = Intent(context, OutboxService::class.java)
                    if (call.argument<Boolean>("active") == true) {
                        if (Build.VERSION.SDK_INT >= 26) context.startForegroundService(intent) else context.startService(intent)
                    } else context.stopService(intent)
                    result.success(null)
                } catch (_: Exception) { result.error("background", "Background sending unavailable", null) }
            }
        }
    }
    fun timeout() { channel?.invokeMethod("timeout", null) }
}

class OutboxService : Service() {
    private var wake: PowerManager.WakeLock? = null
    override fun onCreate() {
        super.onCreate()
        if (Build.VERSION.SDK_INT >= 26) getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel("outbox", "消息发送 / Sending messages", NotificationManager.IMPORTANCE_LOW))
        wake = (getSystemService(POWER_SERVICE) as PowerManager).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "AgentDeck:outbox")
    }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val content = PendingIntent.getActivity(this, 8044, Intent(this, MainActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        val builder = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(this, "outbox") else Notification.Builder(this)
        startForeground(8044, builder.setSmallIcon(android.R.drawable.stat_sys_upload)
            .setContentTitle("AgentDeck · 消息待发送 / Sending messages")
            .setContentText("离开会话后继续发送，点此查看 / Tap to view pending messages")
            .setContentIntent(content).setOngoing(true).setOnlyAlertOnce(true).setProgress(0, 0, true).build())
        if (wake?.isHeld != true) wake?.acquire(6 * 60 * 60 * 1000L)
        (application as DeckApplication).engine
        return START_STICKY
    }
    override fun onTimeout(startId: Int, fgsType: Int) { OutboxBridge.timeout(); stopSelf() }
    override fun onDestroy() { if (wake?.isHeld == true) wake?.release(); super.onDestroy() }
    override fun onBind(intent: Intent?): IBinder? = null
}
