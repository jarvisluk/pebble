import java.io.StringReader
import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

val versionPropsText = providers.fileContents(
    rootProject.layout.projectDirectory.file("version.properties")
).asText.orNull ?: error("Missing apps/android/version.properties; run scripts/version.py sync")
val versionProps = Properties().apply { load(StringReader(versionPropsText)) }
val appVersionName = versionProps.getProperty("VERSION_NAME")?.trim()?.takeIf { it.isNotEmpty() }
    ?: error("Missing VERSION_NAME in apps/android/version.properties")
val appVersionCode = versionProps.getProperty("VERSION_CODE")?.trim()?.toIntOrNull()
    ?: error("Invalid VERSION_CODE in apps/android/version.properties")

android {
    namespace = "ai.pebble.android"
    compileSdk {
        version = release(37) {
            minorApiLevel = 2
        }
    }

    defaultConfig {
        applicationId = "ai.pebble.android"
        minSdk = 26
        targetSdk = 36
        versionCode = appVersionCode
        versionName = appVersionName

        val abiProp = providers.gradleProperty("pebble.abis").orNull
        ndk {
            abiFilters += if (abiProp.isNullOrBlank()) {
                listOf("arm64-v8a", "x86_64")
            } else {
                abiProp.split(",").map { it.trim() }.filter { it.isNotEmpty() }
            }
        }
    }

    val releaseKeystorePath = providers.environmentVariable("PEBBLE_KEYSTORE_PATH").orNull?.trim()
    val releaseKeystorePassword = providers.environmentVariable("PEBBLE_KEYSTORE_PASSWORD").orNull
    val releaseKeyAlias = providers.environmentVariable("PEBBLE_KEY_ALIAS").orNull
    val releaseKeyPassword = providers.environmentVariable("PEBBLE_KEY_PASSWORD").orNull

    signingConfigs {
        if (!releaseKeystorePath.isNullOrBlank()) {
            create("release") {
                storeFile = file(releaseKeystorePath)
                storePassword = releaseKeystorePassword
                keyAlias = releaseKeyAlias
                keyPassword = releaseKeyPassword
            }
        }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            signingConfig = if (!releaseKeystorePath.isNullOrBlank()) {
                signingConfigs.getByName("release")
            } else {
                signingConfigs.getByName("debug")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
        aidl = false
        shaders = false
        resValues = false
    }

    lint {
        checkReleaseBuilds = false
    }

    packaging {
        jniLibs {
            useLegacyPackaging = false
        }
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.material3)
    debugImplementation(libs.androidx.compose.ui.tooling)

    implementation(libs.okhttp)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)

    implementation(libs.coil.compose)
    implementation(libs.coil.network.okhttp)

    implementation(libs.androidx.camera.core)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    implementation(libs.mlkit.barcode.scanning)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
}
