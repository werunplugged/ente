import 'dart:async';

import 'package:flutter/material.dart';
import 'package:photos/core/event_bus.dart';
import 'package:photos/events/opened_settings_event.dart';
import 'package:photos/events/sync_status_update_event.dart';
import 'package:photos/events/user_details_changed_event.dart';
import 'package:photos/models/user_details.dart';
import 'package:photos/services/account/user_service.dart';

class UserDetailsStateWidget extends StatefulWidget {
  final Widget child;

  const UserDetailsStateWidget({
    required this.child,
    super.key,
  });

  @override
  State<UserDetailsStateWidget> createState() => UserDetailsStateWidgetState();
}

class UserDetailsStateWidgetState extends State<UserDetailsStateWidget> {
  late UserDetails? _userDetails;
  late StreamSubscription<OpenedSettingsEvent> _openedSettingsEventSubscription;
  late StreamSubscription<UserDetailsChangedEvent>
      _userDetailsChangedSubscription;
  late StreamSubscription<SyncStatusUpdate> _syncStatusSubscription;
  bool _isCached = true;

  @override
  void initState() {
    _userDetails = UserService.instance.getCachedUserDetails();
    _openedSettingsEventSubscription =
        Bus.instance.on<OpenedSettingsEvent>().listen((event) {
      _fetchUserDetails();
    });
    _userDetailsChangedSubscription =
        Bus.instance.on<UserDetailsChangedEvent>().listen((event) {
      _refreshFromCache();
    });
    // UNP-6647: refetch after a backup batch completes so the memory count
    // stays current during and after high-volume uploads. The server
    // recomputes usage/count asynchronously, so fetch again shortly after —
    // the second response lands once the server has caught up.
    _syncStatusSubscription =
        Bus.instance.on<SyncStatusUpdate>().listen((event) {
      if (event.status == SyncStatus.completedBackup) {
        _fetchUserDetails();
        Future.delayed(const Duration(seconds: 10), () {
          if (mounted) {
            _fetchUserDetails();
          }
        });
      }
    });
    super.initState();
  }

  @override
  void dispose() {
    _openedSettingsEventSubscription.cancel();
    _userDetailsChangedSubscription.cancel();
    _syncStatusSubscription.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => InheritedUserDetails(
        userDetailsState: this,
        userDetails: _userDetails,
        isCached: _isCached,
        child: widget.child,
      );

  void _fetchUserDetails() async {
    _userDetails = await UserService.instance.getUserDetailsV2(
      memoryCount: true,
      shouldCache: true,
    );
    _isCached = false;
    if (mounted) {
      setState(() {});
    }
  }

  void _refreshFromCache() {
    _userDetails = UserService.instance.getCachedUserDetails();
    if (mounted) {
      setState(() {});
    }
  }
}

class InheritedUserDetails extends InheritedWidget {
  final UserDetailsStateWidgetState userDetailsState;
  final UserDetails? userDetails;
  final bool isCached;

  const InheritedUserDetails({
    super.key,
    required super.child,
    required this.userDetails,
    required this.isCached,
    required this.userDetailsState,
  });

  static InheritedUserDetails? of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<InheritedUserDetails>();

  @override
  bool updateShouldNotify(covariant InheritedUserDetails oldWidget) {
    return (userDetails != oldWidget.userDetails) ||
        (isCached != oldWidget.isCached);
  }
}
