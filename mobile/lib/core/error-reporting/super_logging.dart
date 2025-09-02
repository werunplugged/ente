library super_logging;

import 'dart:async';
import 'dart:collection';
import 'dart:core';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:intl/intl.dart';
import 'package:logging/logging.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path/path.dart';
import 'package:path_provider/path_provider.dart';
import 'package:photos/models/typedefs.dart';
import "package:photos/utils/device_info.dart";
import "package:photos/utils/ram_check_util.dart";
import 'package:shared_preferences/shared_preferences.dart';

extension SuperString on String {
  Iterable<String> chunked(int chunkSize) sync* {
    var start = 0;

    while (true) {
      final stop = start + chunkSize;
      if (stop > length) break;
      yield substring(start, stop);
      start = stop;
    }

    if (start < length) {
      yield substring(start);
    }
  }
}

extension SuperLogRecord on LogRecord {
  String toPrettyString([String? extraLines, bool inIsolate = false]) {
    final header =
        "[$loggerName${inIsolate ? " (in isolate)" : ""}] [$level] [$time]";

    var msg = "$header $message";

    if (error != null) {
      msg += "\n⤷ type: ${error.runtimeType}\n⤷ error: $error";
    }
    if (stackTrace != null) {
      msg += "\n⤷ trace: $stackTrace";
    }

    for (var line in extraLines?.split('\n') ?? []) {
      msg += '\n$header $line';
    }

    return msg;
  }
}

class LogConfig {
  String? tunnel;

  /// Path of the directory where log files will be stored.
  ///
  /// If this is [null], file logging is completely disabled (default).
  ///
  /// If this is an empty string (['']),
  /// then a 'logs' directory will be created in [getTemporaryDirectory()].
  ///
  /// A non-empty string will be treated as an explicit path to a directory.
  ///
  /// The chosen directory can be accessed using [SuperLogging.logFile.parent].
  String? logDirPath;

  /// The maximum number of log files inside [logDirPath].
  ///
  /// One log file is created per day.
  /// Older log files are deleted automatically.
  int maxLogFiles;

  /// Whether to enable super logging features in debug mode.
  bool enableInDebugMode;

  /// If provided, super logging will invoke this function, and
  /// any uncaught errors during its execution will be reported.
  ///
  /// Works by using [FlutterError.onError] and [runZoned].
  FutureOrVoidCallback? body;

  /// The date format for storing log files.
  ///
  /// `DateFormat('y-M-d')` by default.
  DateFormat? dateFmt;

  String prefix;

  LogConfig({
    this.tunnel,
    this.logDirPath,
    this.maxLogFiles = 10,
    this.enableInDebugMode = false,
    this.body,
    this.dateFmt,
    this.prefix = "",
  }) {
    dateFmt ??= DateFormat("y-M-d");
  }
}

class SuperLogging {
  /// The logger for SuperLogging
  static final $ = Logger('ente_logging');

  /// The current super logging configuration
  static late LogConfig config;

  static late SharedPreferences _preferences;

  static const keyShouldReportCrashes = "should_report_crashes";

  static Future<void> main([LogConfig? appConfig]) async {
    appConfig ??= LogConfig();
    SuperLogging.config = appConfig;

    WidgetsFlutterBinding.ensureInitialized();
    _preferences = await SharedPreferences.getInstance();

    appVersion ??= await getAppVersion();
    final isFDroidClient = await isFDroidBuild();
    if (isFDroidClient) {
      appConfig.tunnel = null;
    }

    final enable = appConfig.enableInDebugMode || kReleaseMode;
    fileIsEnabled = enable && appConfig.logDirPath != null;

    if (fileIsEnabled) {
      await setupLogDir();
    }

    Logger.root.level = Level.ALL;
    Logger.root.onRecord.listen(onLogRecord);

    if (fileIsEnabled) {
      $.info("log file for today: $logFile with prefix ${appConfig.prefix}");
    }

    unawaited(
      getDeviceInfo().then((info) {
        $.info("Device Info: $info");
      }),
    );

    unawaited(
      checkDeviceTotalRAM().then((ram) {
        if (ram != null) $.info("Device RAM: ${ram}MB");
      }),
    );

    if (appConfig.body == null) return;

    if (!enable) {
      await appConfig.body!();
    }
  }

  static String _lastExtraLines = '';

  static Future onLogRecord(LogRecord rec) async {
    // log misc info if it changed
    String? extraLines = "app version: '$appVersion'\n";
    if (extraLines != _lastExtraLines) {
      _lastExtraLines = extraLines;
    } else {
      extraLines = null;
    }

    final str = (config.prefix) + " " + rec.toPrettyString(extraLines);

    // write to stdout
    printLog(str);

    saveLogString(str, rec.error);
  }

  static void saveLogString(String str, Object? error) {
    // push to log queue
    if (fileIsEnabled) {
      fileQueueEntries.add(str + '\n');
      if (fileQueueEntries.length == 1) {
        flushQueue();
      }
    }
  }

  static final Queue<String> fileQueueEntries = Queue();
  static bool isFlushing = false;

  static void flushQueue() async {
    if (isFlushing || logFile == null) {
      return;
    }
    isFlushing = true;
    final entry = fileQueueEntries.removeFirst();
    await logFile!.writeAsString(entry, mode: FileMode.append, flush: true);
    isFlushing = false;
    if (fileQueueEntries.isNotEmpty) {
      flushQueue();
    }
  }

  // Logs on must be chunked or they get truncated otherwise
  // See https://github.com/flutter/flutter/issues/22665
  static var logChunkSize = 800;

  static void printLog(String text) {
    if (kDebugMode) {
      // ignore: avoid_print
      text.chunked(logChunkSize).forEach(print);
    }
  }

  static bool shouldReportCrashes() {
    if (_preferences.containsKey(keyShouldReportCrashes)) {
      return _preferences.getBool(keyShouldReportCrashes)!;
    } else {
      return true; // Report crashes by default
    }
  }

  /// The log file currently in use.
  static File? logFile;

  /// Whether file logging is currently enabled or not.
  static bool fileIsEnabled = false;

  static Future<void> setupLogDir() async {
    var dirPath = config.logDirPath;

    // choose [logDir]
    if (dirPath == null || dirPath.isEmpty) {
      final root = await getExternalStorageDirectory();
      dirPath = '${root!.path}/logs';
    }

    // create [logDir]
    final dir = Directory(dirPath);
    await dir.create(recursive: true);

    final files = <File>[];
    final dates = <File, DateTime>{};

    // collect all log files with valid names
    await for (final file in dir.list()) {
      try {
        final date = config.dateFmt!.parse(basename(file.path));
        dates[file as File] = date;
        files.add(file);
      } on FormatException catch (_) {}
    }
    final nowTime = DateTime.now();

    // delete old log files, if [maxLogFiles] is exceeded.
    if (files.length > config.maxLogFiles) {
      // sort files based on ascending order of date (older first)
      files.sort(
        (a, b) => (dates[a] ?? nowTime).compareTo((dates[b] ?? nowTime)),
      );

      final extra = files.length - config.maxLogFiles;
      final toDelete = files.sublist(0, extra);

      for (final file in toDelete) {
        try {
          $.fine(
            "deleting log file ${file.path}",
          );
          await file.delete();
        } catch (_) {}
      }
    }

    logFile = File("$dirPath/${config.dateFmt!.format(DateTime.now())}.log");
  }

  /// Current app version, obtained from package_info plugin.
  ///
  /// See: [getAppVersion]
  static String? appVersion;

  static Future<String> getAppVersion() async {
    final pkgInfo = await PackageInfo.fromPlatform();
    return "${pkgInfo.version}+${pkgInfo.buildNumber}";
  }

  static Future<bool> isFDroidBuild() async {
    if (!Platform.isAndroid) {
      return false;
    }
    final pkgName = (await PackageInfo.fromPlatform()).packageName;
    return pkgName.startsWith("io.ente.photos.fdroid");
  }
}
