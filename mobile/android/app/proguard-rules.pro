-keep class ai.onnxruntime.** { *; }
# To ensure that stack traces is unambiguous
# https://developer.android.com/studio/build/shrink-code#decode-stack-trace
-keepattributes LineNumberTable,SourceFile

-keep class org.chromium.net.** { *; }
-keep class org.xmlpull.v1.** { *; }

# Flutter logging package rules
-keep class dart.** { *; }
-keep class io.flutter.** { *; }
-keep class io.flutter.plugins.** { *; }

# Keep the logging package classes
-keep class logging.** { *; }
-keep class dart.logging.** { *; }

# Keep Flutter engine classes
-keep class io.flutter.embedding.** { *; }
-keep class io.flutter.view.** { *; }

# Keep your app's main classes
-keep class io.flutter.app.** { *; }
-keep class io.flutter.plugin.** { *; }

# Keep Flutter native method channels
-keepclassmembers class * {
    @io.flutter.plugin.common.MethodChannel$MethodCallHandler <methods>;
}

# Exclude Google Play Services classes (for independent builds)
-dontwarn com.google.android.play.core.**
-keep class com.google.android.play.core.** { *; }

# Remove all debug logging in release builds
-assumenosideeffects class android.util.Log {
    public static *** d(...);
    public static *** v(...);
    public static *** i(...);
    public static *** w(...);
    public static *** e(...);
    public static *** wtf(...);
}

# Remove System.out.print calls
-assumenosideeffects class java.lang.System {
    public static void out.println(...);
    public static void err.println(...);
    public static void out.print(...);
    public static void err.print(...);
}

# Remove Kotlin print functions
-assumenosideeffects class kotlin.io.ConsoleKt {
    public static *** println(...);
    public static *** print(...);
}

# Remove Flutter/Dart print calls
-assumenosideeffects class io.flutter.Log {
    public static *** d(...);
    public static *** v(...);
    public static *** i(...);
    public static *** w(...);
    public static *** e(...);
    public static *** wtf(...);
}

# Remove Flutter print() calls via System.out
-assumenosideeffects class java.io.PrintStream {
    public void print(...);
    public void println(...);
}