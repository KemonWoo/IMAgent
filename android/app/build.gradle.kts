plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.linscm.imagent"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.linscm.imagent"
        minSdk = 26
        targetSdk = 34
        versionCode = 1
        versionName = "1.0.0"
        vectorDrawables.useSupportLibrary = true
    }

    signingConfigs {
        create("release") {
            storeFile = file(System.getProperty("user.home") + "/imagent-release.keystore")
            storePassword = "android"
            keyAlias = "imagent"
            keyPassword = "android"
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    // sherpa-onnx v1.13.2 — on-device STT + TTS + VAD (local AAR)
    implementation(files("libs/sherpa-onnx-1.13.2.aar"))

    // ZXing — QR code scanning
    implementation("com.google.zxing:core:3.5.3")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")

    // AndroidX core
    implementation("androidx.core:core-ktx:1.12.0")
    implementation("androidx.appcompat:appcompat:1.6.1")
    implementation("com.google.android.material:material:1.11.0")

    // WebSocket client for MCP connection
    implementation("org.java-websocket:Java-WebSocket:1.5.6")

    // JSON parsing
    implementation("com.google.code.gson:gson:2.10.1")
}
