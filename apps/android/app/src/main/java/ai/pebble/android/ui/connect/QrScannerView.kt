package ai.pebble.android.ui.connect

import android.Manifest
import android.content.pm.PackageManager
import android.view.HapticFeedbackConstants
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.Camera
import androidx.camera.core.CameraSelector
import androidx.camera.core.FocusMeteringAction
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.graphics.ClipOp
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.clipPath
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import ai.pebble.android.i18n.L
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import kotlinx.coroutines.delay
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

private fun isValidPairingQr(code: String): Boolean {
    val lower = code.lowercase()
    return lower.startsWith("tailcat://") ||
        lower.startsWith("tailscale://") ||
        lower.startsWith("http://") ||
        lower.startsWith("https://")
}

@Composable
fun QrScannerBox(
    onCode: (String) -> Unit,
    onMessage: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val view = LocalView.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val p = LocalPalette.current

    var hasCameraPerm by remember {
        mutableStateOf(
            ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED
        )
    }
    var torchOn by remember { mutableStateOf(false) }
    var cameraRef by remember { mutableStateOf<Camera?>(null) }
    var cameraProviderRef by remember { mutableStateOf<ProcessCameraProvider?>(null) }
    var focusIndicator by remember { mutableStateOf<Offset?>(null) }
    var bottomBarHeightPx by remember { mutableIntStateOf(0) }
    val locked = remember { AtomicBoolean(false) }
    val disposed = remember { AtomicBoolean(false) }

    fun releaseCamera() {
        torchOn = false
        runCatching { cameraRef?.cameraControl?.enableTorch(false) }
        cameraRef = null
        runCatching { cameraProviderRef?.unbindAll() }
        cameraProviderRef = null
    }

    val scanner = remember {
        val opts = BarcodeScannerOptions.Builder()
            .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
            .build()
        BarcodeScanning.getClient(opts)
    }

    DisposableEffect(Unit) {
        onDispose {
            disposed.set(true)
            releaseCamera()
            runCatching { scanner.close() }
        }
    }

    LaunchedEffect(focusIndicator) {
        if (focusIndicator != null) {
            delay(800)
            focusIndicator = null
        }
    }

    val permLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        hasCameraPerm = granted
        if (!granted) {
            onMessage(L.t("camPermReq"))
        }
    }

    val photoPicker = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        runCatching {
            val image = InputImage.fromFilePath(context, uri)
            scanner.process(image)
                .addOnSuccessListener { barcodes ->
                    val raw = barcodes.firstNotNullOfOrNull { it.rawValue }?.trim().orEmpty()
                    if (raw.isNotEmpty() && isValidPairingQr(raw)) {
                        releaseCamera()
                        view.performHapticFeedback(HapticFeedbackConstants.CONFIRM)
                        onCode(raw)
                    } else if (raw.isNotEmpty()) {
                        onMessage(L.t("invalidQr"))
                    } else {
                        onMessage(L.t("scanNoQrFound"))
                    }
                }
                .addOnFailureListener {
                    onMessage(L.t("scanNoQrFound"))
                }
        }.onFailure {
            onMessage(L.t("scanNoQrFound"))
        }
    }

    LaunchedEffect(Unit) {
        if (!hasCameraPerm) {
            permLauncher.launch(Manifest.permission.CAMERA)
        }
    }

    Box(
        modifier = modifier
            .clip(RoundedCornerShape(16.dp))
            .background(Color.Black),
    ) {
        if (hasCameraPerm) {
            val executor = remember { Executors.newSingleThreadExecutor() }
            DisposableEffect(Unit) {
                onDispose { executor.shutdown() }
            }

            AndroidView(
                modifier = Modifier.fillMaxSize(),
                factory = { ctx ->
                    val pv = PreviewView(ctx).apply {
                        implementationMode = PreviewView.ImplementationMode.COMPATIBLE
                        scaleType = PreviewView.ScaleType.FILL_CENTER
                    }
                    val future = ProcessCameraProvider.getInstance(ctx)
                    future.addListener({
                        val provider = runCatching { future.get() }.getOrNull() ?: return@addListener
                        if (disposed.get()) return@addListener
                        cameraProviderRef = provider
                        val preview = Preview.Builder().build().also {
                            it.surfaceProvider = pv.surfaceProvider
                        }
                        val analysis = ImageAnalysis.Builder()
                            .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                            .build()
                        analysis.setAnalyzer(executor) { imageProxy ->
                            val mediaImage = imageProxy.image
                            if (mediaImage == null) {
                                imageProxy.close()
                                return@setAnalyzer
                            }
                            val input = InputImage.fromMediaImage(
                                mediaImage,
                                imageProxy.imageInfo.rotationDegrees,
                            )
                            scanner.process(input)
                                .addOnSuccessListener { barcodes ->
                                    val code = barcodes.firstNotNullOfOrNull { it.rawValue }?.trim()
                                    if (!code.isNullOrEmpty() && !locked.get()) {
                                        val isValid = isValidPairingQr(code)
                                        if (isValid && locked.compareAndSet(false, true)) {
                                            pv.post {
                                                releaseCamera()
                                                view.performHapticFeedback(HapticFeedbackConstants.CONFIRM)
                                                onCode(code)
                                            }
                                        } else if (!isValid) {
                                            pv.post { onMessage(L.t("invalidQr")) }
                                        }
                                    }
                                }
                                .addOnCompleteListener {
                                    imageProxy.close()
                                }
                        }

                        runCatching {
                            if (disposed.get()) {
                                provider.unbindAll()
                                return@addListener
                            }
                            provider.unbindAll()
                            val cam = provider.bindToLifecycle(
                                lifecycleOwner,
                                CameraSelector.DEFAULT_BACK_CAMERA,
                                preview,
                                analysis,
                            )
                            if (disposed.get()) {
                                runCatching { cam.cameraControl.enableTorch(false) }
                                provider.unbindAll()
                            } else {
                                cameraRef = cam
                            }
                        }
                    }, ContextCompat.getMainExecutor(ctx))

                    val scaleDetector = ScaleGestureDetector(
                        ctx,
                        object : ScaleGestureDetector.SimpleOnScaleGestureListener() {
                            override fun onScale(detector: ScaleGestureDetector): Boolean {
                                val cam = cameraRef ?: return false
                                val cur = cam.cameraInfo.zoomState.value ?: return false
                                val next = (cur.zoomRatio * detector.scaleFactor)
                                    .coerceIn(cur.minZoomRatio, cur.maxZoomRatio)
                                cam.cameraControl.setZoomRatio(next)
                                return true
                            }
                        },
                    )
                    pv.setOnTouchListener { v, event ->
                        scaleDetector.onTouchEvent(event)
                        if (event.action == MotionEvent.ACTION_UP && event.pointerCount == 1 && !scaleDetector.isInProgress) {
                            val cam = cameraRef
                            if (cam != null) {
                                val point = pv.meteringPointFactory.createPoint(event.x, event.y)
                                val action = FocusMeteringAction.Builder(point, FocusMeteringAction.FLAG_AF)
                                    .setAutoCancelDuration(3, TimeUnit.SECONDS)
                                    .build()
                                cam.cameraControl.startFocusAndMetering(action)
                                focusIndicator = Offset(event.x, event.y)
                            }
                            v.performClick()
                        }
                        true
                    }
                    pv
                },
            )

            // Viewfinder mask + corner frame
            Canvas(modifier = Modifier.fillMaxSize()) {
                val topPad = 16.dp.toPx()
                val gapAboveBar = 10.dp.toPx()
                val effectiveBottomBar = if (bottomBarHeightPx > 0) bottomBarHeightPx.toFloat() else 88.dp.toPx()
                val availHeight = (size.height - effectiveBottomBar - topPad - gapAboveBar).coerceAtLeast(64.dp.toPx())
                val availWidth = (size.width - 32.dp.toPx()).coerceAtLeast(64.dp.toPx())
                val frameSide = minOf(availWidth * 0.70f, availHeight * 0.90f).coerceIn(72.dp.toPx(), 196.dp.toPx())
                val left = (size.width - frameSide) / 2f
                val top = topPad + (availHeight - frameSide) / 2f
                val rect = Rect(left, top, left + frameSide, top + frameSide)
                val cutout = Path().apply {
                    addRoundRect(RoundRect(rect, CornerRadius(16.dp.toPx(), 16.dp.toPx())))
                }
                clipPath(cutout, clipOp = ClipOp.Difference) {
                    drawRect(Color.Black.copy(alpha = 0.45f))
                }
                drawRoundRect(
                    color = Color.White.copy(alpha = 0.85f),
                    topLeft = rect.topLeft,
                    size = rect.size,
                    cornerRadius = CornerRadius(16.dp.toPx(), 16.dp.toPx()),
                    style = Stroke(width = 2.dp.toPx()),
                )
                focusIndicator?.let { pt ->
                    drawCircle(
                        color = Color.White.copy(alpha = 0.9f),
                        radius = 22.dp.toPx(),
                        center = pt,
                        style = Stroke(width = 2.dp.toPx()),
                    )
                }
            }

            // Bottom floating pills: hint + torch + album picker
            Column(
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .fillMaxWidth()
                    .onSizeChanged { bottomBarHeightPx = it.height }
                    .padding(horizontal = 14.dp, vertical = 12.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = L.t("scanHint"),
                    color = Color.White.copy(alpha = 0.90f),
                    fontSize = PebbleFont.caption,
                    lineHeight = 16.sp,
                    maxLines = 2,
                    textAlign = TextAlign.Center,
                )
                Row(
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    if (cameraRef?.cameraInfo?.hasFlashUnit() == true) {
                        ScannerPillButton(
                            icon = {
                                Icon(
                                    painter = painterResource(if (torchOn) PebbleIcons.FlashlightOn else PebbleIcons.FlashlightOff),
                                    contentDescription = L.t("scanTorch"),
                                    tint = Color.White,
                                    modifier = Modifier.size(16.dp),
                                )
                            },
                            label = L.t("scanTorch"),
                            onClick = {
                                val next = !torchOn
                                torchOn = next
                                cameraRef?.cameraControl?.enableTorch(next)
                            },
                        )
                    }
                    ScannerPillButton(
                        icon = {
                            Icon(
                                painter = painterResource(PebbleIcons.PhotoLibrary),
                                contentDescription = L.t("scanPickPhoto"),
                                tint = Color.White,
                                modifier = Modifier.size(16.dp),
                            )
                        },
                        label = L.t("scanPickPhoto"),
                        onClick = {
                            photoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
                        },
                    )
                }
            }
        } else {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(20.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                Text(
                    text = L.t("scanAlbumFallback"),
                    color = Color.White.copy(alpha = 0.85f),
                    fontSize = PebbleFont.secondary,
                    textAlign = TextAlign.Center,
                )
                Spacer(Modifier.height(14.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    Button(
                        onClick = { permLauncher.launch(Manifest.permission.CAMERA) },
                        colors = ButtonDefaults.buttonColors(containerColor = p.card, contentColor = p.text),
                    ) {
                        Text(L.t("scanGrantCamera"), fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold)
                    }
                    Button(
                        onClick = {
                            photoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
                        },
                        colors = ButtonDefaults.buttonColors(containerColor = Color.White.copy(alpha = 0.2f), contentColor = Color.White),
                    ) {
                        Text(L.t("scanPickPhoto"), fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold)
                    }
                }
            }
        }
    }
}

@Composable
private fun ScannerPillButton(
    icon: @Composable () -> Unit,
    label: String,
    onClick: () -> Unit,
) {
    Surface(
        color = Color.Black.copy(alpha = 0.65f),
        shape = CircleShape,
        modifier = Modifier
            .border(1.dp, Color.White.copy(alpha = 0.22f), CircleShape)
            .clip(CircleShape)
            .clickable(onClick = onClick),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 7.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            icon()
            Text(text = label, color = Color.White, fontSize = PebbleFont.secondary, fontWeight = FontWeight.Medium)
        }
    }
}
