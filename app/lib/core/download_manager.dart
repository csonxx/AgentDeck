import 'dart:async';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'client.dart';
import 'download.dart';

class DownloadTask {
  final String id, session, href, name, mime, binding;
  int received, total;
  String version, status, error;
  DownloadControl? control;
  DownloadTask({
    required this.id,
    required this.session,
    required this.href,
    required this.name,
    required this.mime,
    required this.binding,
    this.received = 0,
    this.total = -1,
    this.version = '',
    this.status = 'paused',
    this.error = '',
  });
  bool get running =>
      status == 'running' || status == 'waiting' || status == 'pausing';
  bool get complete => status == 'complete';
  Map<String, dynamic> toJson() => {
    'id': id,
    'session': session,
    'href': href,
    'name': name,
    'mime': mime,
    'binding': binding,
    'received': received,
    'total': total,
    'version': version,
    'status': status,
  };
  factory DownloadTask.fromJson(Map<String, dynamic> v) => DownloadTask(
    id: v['id'] as String,
    session: v['session'] as String,
    href: v['href'] as String,
    name: v['name'] as String,
    mime: v['mime'] as String,
    binding: v['binding'] as String,
    received: v['received'] as int,
    total: v['total'] as int,
    version: v['version'] as String? ?? '',
    status: v['status'] as String,
  );
}

class DownloadManager extends ChangeNotifier {
  final AgentClient client;
  final List<DownloadTask> tasks = [];
  Future<void>? _restoring;
  String restoreError = '';
  Future<void> _saveQueue = Future.value();
  bool _disposed = false;
  bool _background = false;
  DownloadManager(this.client) {
    if (!kIsWeb && Platform.isAndroid) {
      DownloadDestination.channel.setMethodCallHandler((call) async {
        if (call.method == 'backgroundTimeout') {
          for (final task in tasks.where((t) => t.running)) {
            pause(task);
          }
        }
      });
    }
  }
  String get binding => client.device?['device'] as String? ?? 'unpaired';
  DownloadTask? forLink(String session, String href) => tasks
      .where(
        (t) => t.binding == binding && t.session == session && t.href == href,
      )
      .lastOrNull;
  Future<void> restore() => _restoring ??= _restore().catchError((Object _) {
    restoreError = '下载记录读取失败，请重新打开应用后重试';
    _restoring = null;
    if (!_disposed) notifyListeners();
  });
  Future<void> _restore() async {
    final rows = await client.vault.loadDownloads();
    restoreError = '';
    if (_disposed) return;
    final restart = <DownloadTask>[];
    for (final row in rows) {
      final task = DownloadTask.fromJson(Map<String, dynamic>.from(row as Map));
      if (task.binding != binding) continue;
      if (task.running) {
        task.status = 'paused';
        restart.add(task);
      }
      tasks.add(task);
    }
    notifyListeners();
    if (restart.isEmpty && !kIsWeb && Platform.isAndroid) {
      // Stop an orphan sticky service restarted after the final task committed.
      try {
        await DownloadDestination.channel.invokeMethod('background', {
          'active': false,
        });
      } catch (_) {}
    }
    for (final task in restart) {
      unawaited(resume(task));
    }
  }

  Future<void> _save() {
    final rows = tasks.map((t) => t.toJson()).toList();
    final next = _saveQueue.then((_) => client.vault.saveDownloads(rows));
    _saveQueue = next.catchError((Object _) {});
    return next;
  }

  Future<DownloadTask?> start(
    String session,
    String href,
    String name,
    String mime,
    int size,
  ) async {
    await restore();
    if (restoreError.isNotEmpty) throw FileSystemException(restoreError);
    final previous = forLink(session, href);
    if (previous != null && !previous.complete) {
      unawaited(resume(previous));
      return previous;
    }
    final destination = await DownloadDestination.choose(name, mime);
    if (destination == null) return null;
    final task = DownloadTask(
      id: destination.id,
      session: session,
      href: href,
      name: name,
      mime: mime,
      binding: binding,
      total: size > 0 ? size : -1,
    );
    tasks.add(task);
    await _save();
    unawaited(resume(task));
    notifyListeners();
    return task;
  }

  Future<void> resume(DownloadTask task) async {
    if (_disposed ||
        task.running ||
        task.complete ||
        task.binding != binding ||
        task.control != null) {
      return;
    }
    final control = DownloadControl();
    task.control = control;
    task.status = 'waiting';
    task.error = '';
    client.retainDownloadConnection();
    final destination = DownloadDestination(task.id);
    notifyListeners();
    try {
      await _save();
      await _service(true);
      await destination.resume(task.received);
      for (var attempt = 0; ; attempt++) {
        control.check();
        try {
          task.status = 'running';
          notifyListeners();
          await client.downloadAttachment(
            task.session,
            task.href,
            startOffset: task.received,
            expectedSize: task.total,
            version: task.version,
            control: control,
            identity: (version, total) async {
              task.version = version;
              task.total = total;
              await _save();
            },
            restart: () async {
              await destination.resume(0);
              task.received = 0;
              await _save();
            },
            write: (bytes) async {
              await destination.write(bytes);
              task.received += bytes.length;
              await _save();
            },
            progress: (_, total) {
              task.total = total;
              if (!_disposed) notifyListeners();
            },
          );
          break;
        } on HttpException catch (e) {
          if (attempt >= 4 ||
              e.message.contains('变化') ||
              e.message.contains('无效') ||
              e.message.contains('续传')) {
            rethrow;
          }
          task.status = 'waiting';
          notifyListeners();
          await Future<void>.delayed(Duration(seconds: 1 << attempt));
        }
      }
      control.check();
      await destination.finish();
      task.status = 'complete';
    } on DownloadCancelled {
      task.status = 'paused';
    } catch (e) {
      task.status = 'paused';
      task.error = e is HttpException && e.message.contains('变化')
          ? '源文件已变化，请重新下载'
          : '下载暂停，已保留进度。检查网络或存储后继续。';
    } finally {
      try {
        await destination.pause();
      } catch (_) {}
      task.control = null;
      client.releaseDownloadConnection();
      try {
        await _save();
      } catch (_) {
        task.error = '下载进度保存失败，请检查手机存储';
      }
      if (!tasks.any((t) => t.running)) {
        try {
          await _service(false);
        } catch (_) {}
      }
      if (!_disposed) notifyListeners();
    }
  }

  void pause(DownloadTask task) {
    task.control?.cancelled = true;
    if (task.control != null) task.status = 'pausing';
    if (!_disposed) notifyListeners();
  }

  Future<void> remove(DownloadTask task) async {
    pause(task);
    while (task.control != null) {
      await Future<void>.delayed(const Duration(milliseconds: 50));
    }
    if (!task.complete) await DownloadDestination(task.id).cancel();
    tasks.remove(task);
    await _save();
    notifyListeners();
  }

  Future<void> restart(DownloadTask task) async {
    if (task.control != null) return;
    task.received = 0;
    task.version = '';
    task.total = -1;
    task.status = 'paused';
    await _save();
    await resume(task);
  }

  Future<void> _service(bool active) async {
    if (_background == active) return;
    // Foreground service owns the cached Flutter engine while the Activity is absent.
    if (!kIsWeb && Platform.isAndroid) {
      await DownloadDestination.channel.invokeMethod('background', {
        'active': active,
      });
    }
    _background = active;
  }

  Future<void> reset() async {
    for (final task in tasks) {
      pause(task);
    }
    while (tasks.any((t) => t.control != null)) {
      await Future<void>.delayed(const Duration(milliseconds: 50));
    }
    tasks.clear();
    await _save();
    _restoring = null;
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    for (final task in tasks) {
      pause(task);
    }
    super.dispose();
  }
}
