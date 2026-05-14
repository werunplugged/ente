import "package:flutter/services.dart";
import "package:logging/logging.dart";
import "package:media_extension/media_extension.dart";
import "package:media_extension/media_extension_action_types.dart";

final _logger = Logger("IntentUtil");

Future<MediaExtentionAction> initIntentAction() async {
  final mediaExtensionPlugin = MediaExtension();
  MediaExtentionAction mediaExtensionAction;
  try {
    mediaExtensionAction = await mediaExtensionPlugin.getIntentAction();
    _logger.info(
      "Raw intent action: ${mediaExtensionAction.action}, type: ${mediaExtensionAction.type}, data: ${mediaExtensionAction.data}",
    );

    // Fix incorrect type detection for content URIs
    if (mediaExtensionAction.action == IntentAction.view &&
        mediaExtensionAction.data != null) {
      final correctedAction = _correctMediaType(mediaExtensionAction);
      if (correctedAction != mediaExtensionAction) {
        _logger.info(
          "Corrected media type from ${mediaExtensionAction.type} to ${correctedAction.type}",
        );
        mediaExtensionAction = correctedAction;
      }
    }
  } on PlatformException catch (e) {
    _logger.warning("PlatformException in getIntentAction: $e");
    mediaExtensionAction = MediaExtentionAction(action: IntentAction.main);
  } catch (error) {
    _logger.severe("Error in getIntentAction: $error");
    mediaExtensionAction = MediaExtentionAction(action: IntentAction.main);
  }

  // When the app is opened via ACTION_VIEW but media permissions are denied,
  // MainActivity clears the intent data to prevent a native crash. This results
  // in action=VIEW with null data/type. Route to FileViewer with a fallback
  // type so it can show a permission error instead of a loading screen.
  if (mediaExtensionAction.action == IntentAction.view &&
      mediaExtensionAction.data == null &&
      mediaExtensionAction.type == null) {
    _logger.info("VIEW intent with no data/type - likely permission denied");
    mediaExtensionAction = MediaExtentionAction(
      action: IntentAction.view,
      type: MediaType.video,
    );
  }

  return mediaExtensionAction;
}

MediaExtentionAction _correctMediaType(MediaExtentionAction original) {
  final data = original.data;
  if (data == null) return original;

  // Check if it's an image content URI
  if (data.contains('content://media/external/images') ||
      data.contains('image/') ||
      data.toLowerCase().contains('.jpg') ||
      data.toLowerCase().contains('.jpeg') ||
      data.toLowerCase().contains('.png') ||
      data.toLowerCase().contains('.gif') ||
      data.toLowerCase().contains('.webp') ||
      data.toLowerCase().contains('.bmp')) {
    return MediaExtentionAction(
      action: original.action,
      type: MediaType.image,
      data: original.data,
    );
  }

  // Check if it's a video content URI
  if (data.contains('content://media/external/video') ||
      data.contains('video/') ||
      data.toLowerCase().contains('.mp4') ||
      data.toLowerCase().contains('.mov') ||
      data.toLowerCase().contains('.avi') ||
      data.toLowerCase().contains('.mkv')) {
    return MediaExtentionAction(
      action: original.action,
      type: MediaType.video,
      data: original.data,
    );
  }

  return original;
}
