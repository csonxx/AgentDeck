package dev.agentdeck.agentdeck

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.DocumentsContract
import android.webkit.MimeTypeMap
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.FileOutputStream
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    override fun provideFlutterEngine(context: Context): FlutterEngine = (application as DeckApplication).engine
    override fun shouldDestroyEngineWithHost() = false
    override fun onResume() { super.onResume(); DownloadBridge.activity = this }
    override fun onDestroy() {
        if (DownloadBridge.activity === this) DownloadBridge.activity = null
        super.onDestroy()
    }
    @Deprecated("Platform activity result callback")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == 8041) DownloadBridge.picked(resultCode, data)
    }
}

// The channel and engine belong to the application, not the preview Activity.
// URI grants live in Android; the URI and committed offset live in Dart's encrypted vault.
object DownloadBridge {
    var activity: MainActivity? = null
    private lateinit var context: Context
    private lateinit var channel: MethodChannel
    private val worker = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private var picker: MethodChannel.Result? = null
    private val streams = mutableMapOf<String, FileOutputStream>()
    fun attach(ctx: Context, engine: FlutterEngine) {
        context = ctx.applicationContext
        channel = MethodChannel(engine.dartExecutor.binaryMessenger, "dev.agentdeck/download")
        channel.setMethodCallHandler { call, result ->
            when (call.method) {
                "choose" -> {
                    val host = activity
                    if (host == null || picker != null) result.error("picker", "File picker unavailable", null)
                    else {
                        picker = result
                        val name = call.argument<String>("name") ?: "attachment"
                        val mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.', "").lowercase())
                            ?: call.argument<String>("mime")?.substringBefore(';') ?: "application/octet-stream"
                        try {
                            if (Build.VERSION.SDK_INT >= 33 && host.checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED)
                                host.requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 8042)
                            host.startActivityForResult(Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
                                addCategory(Intent.CATEGORY_OPENABLE); type = mime
                                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
                                putExtra(Intent.EXTRA_TITLE, name)
                            }, 8041)
                        } catch (_: Exception) { picker = null; result.error("picker", "File picker unavailable", null) }
                    }
                }
                "background" -> {
                    try {
                        if (call.argument<Boolean>("active") == true) {
                            val intent = Intent(context, DownloadService::class.java).putExtra("progress", call.argument<Int>("progress") ?: -1)
                            if (Build.VERSION.SDK_INT >= 26) context.startForegroundService(intent) else context.startService(intent)
                        } else context.stopService(Intent(context, DownloadService::class.java))
                        result.success(null)
                    } catch (_: Exception) { result.error("background", "Background download unavailable", null) }
                }
                "open" -> {
                    try {
                        val uri = document(call.argument<String>("id"))
                        val view = Intent(Intent.ACTION_VIEW).apply {
                            setDataAndType(uri, context.contentResolver.getType(uri) ?: "application/octet-stream")
                            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                        }
                        context.startActivity(Intent.createChooser(view, "打开方式 / Open with").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                        result.success(null)
                    } catch (_: Exception) { result.error("viewer", "No app can open this file", null) }
                }
                "resume", "write", "finish", "pause", "cancel" -> worker.execute {
                    try {
                        val id = call.argument<String>("id") ?: error("Missing document")
                        val uri = document(id)
                        when (call.method) {
                            "resume" -> {
                                streams.remove(id)?.close()
                                val offset = (call.argument<Number>("offset") ?: 0).toLong()
                                check(offset >= 0)
                                val fd = context.contentResolver.openFileDescriptor(uri, "rw") ?: error("Cannot reopen")
                                val stream = android.os.ParcelFileDescriptor.AutoCloseOutputStream(fd)
                                try {
                                    check(stream.channel.size() >= offset)
                                    stream.channel.truncate(offset)
                                    stream.channel.position(offset)
                                    streams[id] = stream
                                } catch (e: Exception) { stream.close(); throw e }
                            }
                            "write" -> {
                                val bytes = call.argument<ByteArray>("bytes") ?: error("Missing bytes")
                                check(bytes.size <= 512 * 1024)
                                val output = streams[id] ?: error("Not open")
                                output.write(bytes)
                                output.fd.sync() // Commit bytes before Dart persists the next offset.
                            }
                            "finish", "pause" -> streams.remove(id)?.close()
                            "cancel" -> {
                                streams.remove(id)?.close()
                                check(DocumentsContract.deleteDocument(context.contentResolver, uri))
                                try { context.contentResolver.releasePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION) } catch (_: Exception) {}
                            }
                        }
                        main.post { result.success(null) }
                    } catch (_: Exception) { main.post { result.error("download", "File operation failed; use a local Downloads folder", null) } }
                }
                else -> result.notImplemented()
            }
        }
    }
    private fun document(value: String?): Uri {
        val uri = Uri.parse(value ?: error("Missing URI"))
        check(uri.scheme == "content" && DocumentsContract.isDocumentUri(context, uri))
        return uri
    }
    fun picked(resultCode: Int, data: Intent?) {
        val result = picker ?: return
        picker = null
        val uri = data?.data
        if (resultCode != Activity.RESULT_OK || uri == null) { result.success(null); return }
        try {
            context.contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
            result.success(uri.toString())
        } catch (_: Exception) { result.error("file", "This folder does not support resumable access", null) }
    }
    fun timeout() { channel.invokeMethod("backgroundTimeout", null) }
}
