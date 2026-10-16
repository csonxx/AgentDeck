plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    buildFeatures { buildConfig = true }
    namespace = "dev.agentdeck.agentdeck"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "dev.agentdeck.agentdeck"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    flavorDimensions += "edition"
    productFlavors {
        create("voice") { dimension = "edition"; manifestPlaceholders["agentdeckEdition"] = "voice" }
        create("lite") { dimension = "edition"; manifestPlaceholders["agentdeckEdition"] = "lite" }
    }

    buildTypes {
        release {
            proguardFiles("proguard-rules.pro")
            // TODO: Add your own signing config for the release build.
            // Signing with the debug keys for now, so `flutter run --release` works.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

// Lite has no offline recognizer or model weights. Keep the same application ID
// and signing identity so switching editions preserves pairing and preferences.
androidComponents {
    onVariants(selector().withFlavor("edition" to "lite")) { variant ->
        variant.packaging.jniLibs.excludes.addAll(
            "**/libonnxruntime.so", "**/libsherpa-onnx-*.so"
        )
    }
}

flutter {
    source = "../.."
}
