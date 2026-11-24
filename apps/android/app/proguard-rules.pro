# Preserve JNI bridge class & methods called from libbothub.so
-keep class bothub.tunnel.BothubTunnelModule { *; }

# Keep kotlinx.serialization models
-keepattributes *Annotation*,InnerClasses
-dontnote kotlinx.serialization.AnnotationsKt
-keepclassmembers class kotlinx.serialization.json.** { *** Companion; }
-keepclasseswithmembers class kotlinx.serialization.json.** { kotlinx.serialization.KSerializer serializer(...); }
-keep,includedescriptorclasses class ai.pebble.android.data.**$$serializer { *; }
-keepclassmembers class ai.pebble.android.data.** { *** Companion; }
-keepclasseswithmembers class ai.pebble.android.data.** { kotlinx.serialization.KSerializer serializer(...); }

# Preserve ML Kit & Firebase ComponentRegistrar constructors/methods under R8 Full Mode
-keep class * implements com.google.firebase.components.ComponentRegistrar {
    <init>();
    *** getComponents();
}
-keep class com.google.mlkit.** { *; }
-keep class com.google.android.gms.internal.mlkit_vision_barcode_bundled.** { *; }
-keep class com.google.android.libraries.barhopper.** { *; }

# OkHttp / MLKit quiet rules
-dontwarn okhttp3.**
-dontwarn okio.**
-dontwarn javax.annotation.**
-dontwarn org.conscrypt.**
