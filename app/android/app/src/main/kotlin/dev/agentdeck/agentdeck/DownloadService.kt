package dev.agentdeck.agentdeck

import android.app.Service
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.os.PowerManager

class DownloadService : Service() {
    private var wake: PowerManager.WakeLock? = null
    override fun onCreate() {
        super.onCreate()
        if (Build.VERSION.SDK_INT >= 26) getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel("downloads", "附件下载 / Downloads", NotificationManager.IMPORTANCE_LOW))
        wake = (getSystemService(POWER_SERVICE) as PowerManager).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "AgentDeck:download")
    }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val launch = Intent(this, MainActivity::class.java).putExtra("downloads", true)
        val content = PendingIntent.getActivity(this, 8043, launch, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        val builder = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(this, "downloads") else Notification.Builder(this)
        val progress = intent?.getIntExtra("progress", -1) ?: -1
        startForeground(8043, builder.setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentTitle("AgentDeck · 下载中 / Downloading")
            .setContentText("返回应用查看下载进度 / Open app for progress")
            .setContentIntent(content).setOngoing(true).setOnlyAlertOnce(true)
            .setProgress(100, progress.coerceAtLeast(0), progress < 0).build())
        if (wake?.isHeld != true) wake?.acquire(6 * 60 * 60 * 1000L)
        (application as DeckApplication).engine // Recreate Dart tasks after system process reclamation.
        return START_STICKY
    }
    override fun onTimeout(startId: Int, fgsType: Int) {
        DownloadBridge.timeout()
        stopSelf()
    }
    override fun onDestroy() { if (wake?.isHeld == true) wake?.release(); super.onDestroy() }
    override fun onBind(intent: Intent?): IBinder? = null
}
