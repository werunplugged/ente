import 'dart:async';

import 'package:flutter/material.dart';
import 'package:lottie/lottie.dart';
import 'package:photos/core/event_bus.dart';
import 'package:photos/ente_theme_data.dart';
import 'package:photos/events/local_import_progress.dart';
import 'package:photos/events/sync_status_update_event.dart';
import "package:photos/generated/l10n.dart";
import "package:photos/service_locator.dart";
import "package:photos/ui/components/buttons/button_widget.dart";
import "package:photos/ui/components/dialog_widget.dart";
import "package:photos/ui/components/models/button_type.dart";
import 'package:photos/ui/settings/backup/backup_folder_selection_page.dart';
import "package:photos/utils/email_util.dart";
import 'package:photos/utils/navigation_util.dart';

class LoadingPhotosWidget extends StatefulWidget {
  const LoadingPhotosWidget({super.key});

  @override
  State<LoadingPhotosWidget> createState() => _LoadingPhotosWidgetState();
}

class _LoadingPhotosWidgetState extends State<LoadingPhotosWidget> {
  static const _stallThreshold = Duration(seconds: 60);

  late StreamSubscription<SyncStatusUpdate> _firstImportEvent;
  StreamSubscription<LocalImportProgressEvent>? _importProgressEvent;
  String? _loadingMessage;
  final importStalled = ValueNotifier(false);
  Timer? _stallTimer;

  void _resetStallTimer() {
    _stallTimer?.cancel();
    if (importStalled.value) importStalled.value = false;
    _stallTimer = Timer(_stallThreshold, () {
      if (mounted) importStalled.value = true;
    });
  }

  @override
  void initState() {
    super.initState();
    _resetStallTimer();
    _firstImportEvent =
        Bus.instance.on<SyncStatusUpdate>().listen((event) async {
      if (mounted && event.status == SyncStatus.completedFirstGalleryImport) {
        if (permissionService.hasGrantedLimitedPermissions()) {
          // Do nothing, let HomeWidget refresh
        } else {
          // ignore: unawaited_futures
          routeToPage(
            context,
            const BackupFolderSelectionPage(
              isOnboarding: true,
              isFirstBackup: true,
            ),
          );
        }
      }
    });
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_importProgressEvent != null) {
      _importProgressEvent!.cancel();
    } else {
      _importProgressEvent =
          Bus.instance.on<LocalImportProgressEvent>().listen((event) {
        _loadingMessage = AppLocalizations.of(context)
            .processingImport(folderName: event.folderName);
        // Per-page progress events fire frequently during active import on
        // any library size; reset the stall timer so the help icon only
        // surfaces if events stop arriving for _stallThreshold (60 s).
        _resetStallTimer();
        if (mounted) {
          setState(() {});
        }
      });
    }
  }

  @override
  void dispose() {
    _firstImportEvent.cancel();
    _importProgressEvent?.cancel();
    _stallTimer?.cancel();
    importStalled.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    _loadingMessage ??= AppLocalizations.of(context).loadingYourPhotos;
    final isLightMode = Theme.of(context).brightness == Brightness.light;
    return Scaffold(
      body: SingleChildScrollView(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                Stack(
                  alignment: Alignment.center,
                  children: [
                    isLightMode
                        ? Image.asset(
                            'assets/loading_photos_background.png',
                            color: Colors.white.withValues(alpha: 0.5),
                            colorBlendMode: BlendMode.modulate,
                          )
                        : Image.asset(
                            'assets/loading_photos_background_dark.png',
                            color: Colors.white.withValues(alpha: 0.25),
                            colorBlendMode: BlendMode.modulate,
                          ),
                    Column(
                      children: [
                        const SizedBox(height: 24),
                        Lottie.asset(
                          'assets/loadingGalleryLottie.json',
                          height: 400,
                        ),
                      ],
                    ),
                  ],
                ),
                Text(
                  _loadingMessage!,
                  style: TextStyle(
                    color: Theme.of(context).colorScheme.subTextColor,
                  ),
                ),
                const SizedBox(height: 32),
                Text(
                  "Private, encrypted cloud storage for your photos.\nOnly you have the keys.",
                  textAlign: TextAlign.center,
                  style: Theme.of(context).textTheme.bodyMedium!.copyWith(
                        color: Theme.of(context).colorScheme.subTextColor,
                      ),
                ),
              ],
            ),
          ),
        ),
      ),
      appBar: AppBar(
        actions: [
          ValueListenableBuilder(
            valueListenable: importStalled,
            builder: (context, value, _) {
              return value
                  ? IconButton(
                      icon: const Icon(Icons.help_outline_outlined),
                      onPressed: () {
                        showDialogWidget(
                          context: context,
                          title: AppLocalizations.of(context).oops,
                          icon: Icons.error_outline_outlined,
                          body: AppLocalizations.of(context)
                              .localSyncErrorMessage,
                          isDismissible: true,
                          buttons: [
                            ButtonWidget(
                              buttonType: ButtonType.primary,
                              labelText:
                                  AppLocalizations.of(context).contactSupport,
                              buttonAction: ButtonAction.second,
                              onTap: () async {
                                await sendLogs(
                                  context,
                                  AppLocalizations.of(context).contactSupport,
                                  "support@ente.io",
                                  postShare: () {},
                                );
                              },
                            ),
                          ],
                        );
                      },
                    )
                  : const SizedBox.shrink();
            },
          ),
        ],
      ),
    );
  }
}
