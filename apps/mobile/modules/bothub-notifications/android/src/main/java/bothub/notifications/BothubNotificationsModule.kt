package bothub.notifications

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import expo.modules.kotlin.Promise
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import kotlin.math.abs

class BothubNotificationsModule : Module() {
  companion object {
    private const val CHANNEL_ACTIVITY = "bothub_activity"
    private const val CHANNEL_ALERTS = "bothub_alerts"
  }

  private val context: Context
    get() = appContext.reactContext ?: throw IllegalStateException("React context unavailable")

  override fun definition() = ModuleDefinition {
    Name("BothubNotifications")

    Function("hasPermission") {
      hasNotificationPermission()
    }

    AsyncFunction("requestPermission") { promise: Promise ->
      if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
        promise.resolve(true)
        return@AsyncFunction
      }
      val permissions = appContext.permissions
      if (permissions == null) {
        promise.resolve(hasNotificationPermission())
        return@AsyncFunction
      }
      permissions.askForPermissions(
        { result ->
          val granted = result[Manifest.permission.POST_NOTIFICATIONS]?.status?.name == "GRANTED"
          promise.resolve(granted || hasNotificationPermission())
        },
        Manifest.permission.POST_NOTIFICATIONS
      )
    }

    Function("updateBotActivity") { botId: String, botName: String, statusText: String, stepCount: Int ->
      if (!hasNotificationPermission()) return@Function
      ensureChannels()
      val title = if (botName.isNotBlank()) botName else botId
      val content = if (stepCount > 0) "$statusText · $stepCount 步" else statusText
      val openIntent = deepLinkPendingIntent("bothub://bot/${Uri.encode(botId)}", botId.hashCode())
      val interruptIntent = deepLinkPendingIntent("bothub://interrupt/${Uri.encode(botId)}", ("int:$botId").hashCode())

      val builder = NotificationCompat.Builder(context, CHANNEL_ACTIVITY)
        .setSmallIcon(android.R.drawable.stat_notify_sync)
        .setContentTitle(title)
        .setContentText(content)
        .setOngoing(true)
        .setOnlyAlertOnce(true)
        .setProgress(0, 0, true)
        .setContentIntent(openIntent)
        .addAction(0, "中断", interruptIntent)
        .setPriority(NotificationCompat.PRIORITY_LOW)

      NotificationManagerCompat.from(context).notify(botNotificationId(botId), builder.build())
    }

    Function("endBotActivity") { botId: String, botName: String, summary: String, notify: Boolean ->
      val mgr = NotificationManagerCompat.from(context)
      mgr.cancel(botNotificationId(botId))
      if (!notify || !hasNotificationPermission()) return@Function
      ensureChannels()
      val title = if (botName.isNotBlank()) botName else botId
      val openIntent = deepLinkPendingIntent("bothub://bot/${Uri.encode(botId)}", ("done:$botId").hashCode())
      val builder = NotificationCompat.Builder(context, CHANNEL_ALERTS)
        .setSmallIcon(android.R.drawable.stat_notify_chat)
        .setContentTitle(title)
        .setContentText(summary.ifBlank { "任务已完成" })
        .setAutoCancel(true)
        .setContentIntent(openIntent)
        .setPriority(NotificationCompat.PRIORITY_DEFAULT)
      mgr.notify(botDoneNotificationId(botId), builder.build())
    }

    Function("showTakeoverAlert") { id: String, reason: String, target: String ->
      if (!hasNotificationPermission()) return@Function
      ensureChannels()
      val resolvedTarget = target.ifBlank { "local" }
      val openIntent = deepLinkPendingIntent(
        "bothub://desktop?target=${Uri.encode(resolvedTarget)}",
        ("tk:$id").hashCode()
      )
      val builder = NotificationCompat.Builder(context, CHANNEL_ALERTS)
        .setSmallIcon(android.R.drawable.ic_dialog_alert)
        .setContentTitle("需要接管远程桌面 ($resolvedTarget)")
        .setContentText(reason.ifBlank { "请前往远程桌面完成操作" })
        .setAutoCancel(true)
        .setOngoing(true)
        .setContentIntent(openIntent)
        .setPriority(NotificationCompat.PRIORITY_HIGH)
      NotificationManagerCompat.from(context).notify(takeoverNotificationId(id), builder.build())
    }

    Function("cancelTakeoverAlert") { id: String ->
      NotificationManagerCompat.from(context).cancel(takeoverNotificationId(id))
    }
  }

  private fun hasNotificationPermission(): Boolean {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
      if (ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
        return false
      }
    }
    return NotificationManagerCompat.from(context).areNotificationsEnabled()
  }

  private fun ensureChannels() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    val mgr = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
    if (mgr.getNotificationChannel(CHANNEL_ACTIVITY) == null) {
      val activityChannel = NotificationChannel(
        CHANNEL_ACTIVITY,
        "任务运行进度",
        NotificationManager.IMPORTANCE_LOW
      )
      mgr.createNotificationChannel(activityChannel)
    }
    if (mgr.getNotificationChannel(CHANNEL_ALERTS) == null) {
      val alertChannel = NotificationChannel(
        CHANNEL_ALERTS,
        "接管与完成提醒",
        NotificationManager.IMPORTANCE_HIGH
      )
      mgr.createNotificationChannel(alertChannel)
    }
  }

  private fun deepLinkPendingIntent(uriStr: String, reqCode: Int): PendingIntent {
    val intent = Intent(Intent.ACTION_VIEW, Uri.parse(uriStr)).apply {
      setPackage(context.packageName)
      flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
    }
    val flags = PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
    return PendingIntent.getActivity(context, reqCode, intent, flags)
  }

  private fun botNotificationId(botId: String): Int = 10000 + abs(botId.hashCode() % 10000)
  private fun botDoneNotificationId(botId: String): Int = 20000 + abs(botId.hashCode() % 10000)
  private fun takeoverNotificationId(id: String): Int = 30000 + abs(id.hashCode() % 10000)
}
