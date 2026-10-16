package dev.agentdeck.agentdeck

import android.app.DownloadManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import androidx.core.content.FileProvider
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.Executors

object UpdateBridge {
    private val worker = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    @Suppress("DEPRECATION")
    private fun code(p: android.content.pm.PackageInfo): Long = if (Build.VERSION.SDK_INT >= 28) p.longVersionCode else p.versionCode.toLong()
    @Suppress("DEPRECATION")
    private fun signatures(p: android.content.pm.PackageInfo): Set<String> {
        val signatures = if (Build.VERSION.SDK_INT >= 28) p.signingInfo?.apkContentsSigners else p.signatures
        return signatures?.map { s -> MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).joinToString("") { "%02x".format(it) } }?.toSet() ?: emptySet()
    }
    private fun row(context: Context, id: Long): Map<String, Any> {
        val manager = context.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
        manager.query(DownloadManager.Query().setFilterById(id)).use { cursor ->
            if (!cursor.moveToFirst()) return mapOf("status" to "missing", "received" to 0L, "total" to 0L)
            fun number(name: String) = cursor.getLong(cursor.getColumnIndexOrThrow(name))
            val status = when (number(DownloadManager.COLUMN_STATUS).toInt()) {
                DownloadManager.STATUS_SUCCESSFUL -> "complete"
                DownloadManager.STATUS_FAILED -> "failed"
                DownloadManager.STATUS_PAUSED -> "paused"
                DownloadManager.STATUS_PENDING -> "pending"
                else -> "downloading"
            }
            return mapOf("status" to status, "received" to number(DownloadManager.COLUMN_BYTES_DOWNLOADED_SO_FAR), "total" to number(DownloadManager.COLUMN_TOTAL_SIZE_BYTES))
        }
    }
    fun attach(context: Context, engine: FlutterEngine) {
        val manager = context.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
        MethodChannel(engine.dartExecutor.binaryMessenger, "dev.agentdeck/update").setMethodCallHandler { call, result ->
            try {
                when (call.method) {
                    "info" -> {
                        val p = context.packageManager.getPackageInfo(context.packageName, 0)
                        result.success(mapOf("version" to p.versionName, "version_code" to code(p), "package" to context.packageName, "sdk" to Build.VERSION.SDK_INT, "abis" to Build.SUPPORTED_ABIS.toList(), "edition" to BuildConfig.FLAVOR))
                    }
                    "start" -> {
                        val uri = Uri.parse(call.argument<String>("url") ?: error("Missing URL"))
                        val hash = call.argument<String>("sha256") ?: error("Missing hash")
                        val version = call.argument<Number>("version_code")?.toLong() ?: error("Missing version")
                        val targetEdition = call.argument<String>("edition") ?: "voice"
                        check(targetEdition in listOf("voice", "lite"))
                        check(uri.scheme == "https" && !uri.host.isNullOrBlank() && uri.userInfo == null && uri.query == null && uri.fragment == null && hash.matches(Regex("[a-f0-9]{64}")) && version > 0)
                        val name = "updates/agentdeck-$version-$hash.apk"
                        val file = File(context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS), name)
                        // Starting again is explicit; DownloadManager owns resume of an existing ID.
                        if (file.exists()) check(file.delete())
                        val request = DownloadManager.Request(uri).setTitle("AgentDeck 更新 / Update")
                            .setMimeType("application/vnd.android.package-archive")
                            .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                            .setAllowedOverMetered(true).setAllowedOverRoaming(false)
                            .setDestinationInExternalFilesDir(context, Environment.DIRECTORY_DOWNLOADS, name)
                        result.success(manager.enqueue(request))
                    }
                    "query" -> result.success(row(context, call.argument<Number>("id")!!.toLong()))
                    "cancel" -> { manager.remove(call.argument<Number>("id")!!.toLong()); result.success(null) }
                    "install" -> {
                        val id = call.argument<Number>("id")!!.toLong()
                        val hash = call.argument<String>("sha256")!!
                        val expectedCode = call.argument<Number>("version_code")!!.toLong()
                        val bytes = call.argument<Number>("bytes")!!.toLong()
                        worker.execute {
                            try {
                                check(row(context, id)["status"] == "complete")
                                val path = manager.query(DownloadManager.Query().setFilterById(id)).use { c ->
                                    check(c.moveToFirst()); Uri.parse(c.getString(c.getColumnIndexOrThrow(DownloadManager.COLUMN_LOCAL_URI))).path ?: error("Missing file")
                                }
                                val file = File(path).canonicalFile
                                val allowed = File(context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS), "updates").canonicalFile
                                check(file.parentFile == allowed && file.length() == bytes)
                                val digest = MessageDigest.getInstance("SHA-256")
                                file.inputStream().use { input -> val buffer = ByteArray(256 * 1024); while (true) { val n = input.read(buffer); if (n < 0) break; digest.update(buffer, 0, n) } }
                                check(digest.digest().joinToString("") { "%02x".format(it) } == hash)
                                @Suppress("DEPRECATION") val flags = if (Build.VERSION.SDK_INT >= 28) PackageManager.GET_SIGNING_CERTIFICATES else PackageManager.GET_SIGNATURES
                                @Suppress("DEPRECATION") val installed = context.packageManager.getPackageInfo(context.packageName, flags)
                                val archive = context.packageManager.getPackageArchiveInfo(file.path, flags or PackageManager.GET_META_DATA) ?: error("Invalid APK")
                                val targetEdition = call.argument<String>("edition") ?: "voice"
                                check(targetEdition in listOf("voice", "lite"))
                                check(archive.applicationInfo?.metaData?.getString("dev.agentdeck.edition") == targetEdition)
                                check(archive.packageName == context.packageName && code(archive) == expectedCode)
                                check(expectedCode > code(installed) || expectedCode == code(installed) && targetEdition != BuildConfig.FLAVOR)
                                check(signatures(installed).isNotEmpty() && signatures(archive) == signatures(installed))
                                main.post {
                                    try {
                                        val host = DownloadBridge.activity ?: error("Open the App first")
                                        if (Build.VERSION.SDK_INT >= 26 && !context.packageManager.canRequestPackageInstalls()) {
                                            host.startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}")))
                                            result.success("permission")
                                        } else {
                                            val uri = FileProvider.getUriForFile(context, "${context.packageName}.updates", file)
                                            host.startActivity(Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive").addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION))
                                            result.success("installer")
                                        }
                                    } catch (_: Exception) { result.error("install", "Could not open Android installer", null) }
                                }
                            } catch (_: Exception) { main.post { result.error("verification", "Update verification failed; download again", null) } }
                        }
                    }
                    else -> result.notImplemented()
                }
            } catch (_: Exception) { result.error("update", "Update operation failed", null) }
        }
    }
}
