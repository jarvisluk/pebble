package ai.pebble.android.notify

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.net.Uri
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import ai.pebble.android.i18n.L
import kotlin.math.abs

object NotificationHelper {
    const val CHANNEL_ACTIVITY = "pebble_activity"
    const val CHANNEL_ALERTS = "pebble_alerts"
    private const val FOREGROUND_ID = 9001

    fun hasNotificationPermission(context: Context): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
                return false
            }
        }
        return NotificationManagerCompat.from(context).areNotificationsEnabled()
    }

    fun ensureChannels(context: Context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val mgr = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        mgr.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ACTIVITY,
                L.t("notifChannelActivity"),
                NotificationManager.IMPORTANCE_LOW,
            ),
        )
        mgr.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ALERTS,
                L.t("notifChannelAlerts"),
                NotificationManager.IMPORTANCE_HIGH,
            ),
        )
    }

    fun buildForegroundNotification(context: Context, botNames: List<String>): Notification {
        ensureChannels(context)
        val openIntent = deepLinkPendingIntent(context, "pebble://", FOREGROUND_ID)
        val title = if (botNames.size == 1) {
            L.t("notifFgSingle", mapOf("name" to botNames.first()))
        } else {
            L.t("notifFgMulti", mapOf("n" to botNames.size))
        }
        val text = if (botNames.isEmpty()) L.t("notifFgIdle") else botNames.joinToString(" · ")
        return NotificationCompat.Builder(context, CHANNEL_ACTIVITY)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle(title)
            .setContentText(text)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(openIntent)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    fun updateBotActivity(context: Context, botId: String, botName: String, statusText: String, stepCount: Int) {
        if (!hasNotificationPermission(context)) return
        ensureChannels(context)
        val title = botName.ifBlank { botId }
        val stepLabel = if (stepCount == 1) L.t("notifStepOne") else L.t("notifSteps", mapOf("n" to stepCount))
        val content = if (stepCount > 0) "$statusText · $stepLabel" else statusText
        val openIntent = deepLinkPendingIntent(context, "pebble://bot/${Uri.encode(botId)}", botId.hashCode())
        val interruptIntent = deepLinkPendingIntent(context, "pebble://interrupt/${Uri.encode(botId)}", "int:$botId".hashCode())
        val builder = NotificationCompat.Builder(context, CHANNEL_ACTIVITY)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle(title)
            .setContentText(content)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setProgress(0, 0, true)
            .setContentIntent(openIntent)
            .addAction(0, L.t("notifInterrupt"), interruptIntent)
            .setPriority(NotificationCompat.PRIORITY_LOW)
        NotificationManagerCompat.from(context).notify(botNotificationId(botId), builder.build())
    }

    fun cancelBotActivity(context: Context, botId: String) {
        NotificationManagerCompat.from(context).cancel(botNotificationId(botId))
    }

    fun endBotActivity(context: Context, botId: String, botName: String, summary: String, notify: Boolean) {
        val mgr = NotificationManagerCompat.from(context)
        mgr.cancel(botNotificationId(botId))
        if (!notify || !hasNotificationPermission(context)) return
        ensureChannels(context)
        val title = botName.ifBlank { botId }
        val openIntent = deepLinkPendingIntent(context, "pebble://bot/${Uri.encode(botId)}", "done:$botId".hashCode())
        val builder = NotificationCompat.Builder(context, CHANNEL_ALERTS)
            .setSmallIcon(android.R.drawable.stat_notify_chat)
            .setContentTitle(title)
            .setContentText(summary.ifBlank { L.t("notifTaskDone") })
            .setAutoCancel(true)
            .setContentIntent(openIntent)
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
        mgr.notify(botDoneNotificationId(botId), builder.build())
    }

    fun showTakeoverAlert(context: Context, id: String, reason: String, target: String) {
        if (!hasNotificationPermission(context)) return
        ensureChannels(context)
        val resolvedTarget = target.ifBlank { "local" }
        val openIntent = deepLinkPendingIntent(
            context,
            "pebble://desktop?target=${Uri.encode(resolvedTarget)}",
            "tk:$id".hashCode(),
        )
        val builder = NotificationCompat.Builder(context, CHANNEL_ALERTS)
            .setSmallIcon(android.R.drawable.ic_dialog_alert)
            .setContentTitle(L.t("notifTakeoverTitle", mapOf("target" to resolvedTarget)))
            .setContentText(reason.ifBlank { L.t("notifTakeoverBody") })
            .setAutoCancel(true)
            .setOngoing(true)
            .setContentIntent(openIntent)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
        NotificationManagerCompat.from(context).notify(takeoverNotificationId(id), builder.build())
    }

    fun cancelTakeoverAlert(context: Context, id: String) {
        NotificationManagerCompat.from(context).cancel(takeoverNotificationId(id))
    }

    fun showApprovalAlert(
        context: Context,
        id: String,
        botId: String,
        botName: String,
        summary: String,
    ) {
        if (!hasNotificationPermission(context)) return
        ensureChannels(context)
        val name = botName.ifBlank { botId }.ifBlank { "Bot" }
        val openIntent = deepLinkPendingIntent(
            context,
            "pebble://bot/${Uri.encode(botId)}",
            "apr:$id".hashCode(),
        )
        val builder = NotificationCompat.Builder(context, CHANNEL_ALERTS)
            .setSmallIcon(android.R.drawable.ic_dialog_alert)
            .setContentTitle(L.t("notifApprovalTitle", mapOf("name" to name)))
            .setContentText(summary.ifBlank { L.t("notifApprovalBody") })
            .setAutoCancel(true)
            .setContentIntent(openIntent)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
        NotificationManagerCompat.from(context).notify(approvalNotificationId(id), builder.build())
    }

    fun cancelApprovalAlert(context: Context, id: String) {
        NotificationManagerCompat.from(context).cancel(approvalNotificationId(id))
    }

    private fun deepLinkPendingIntent(context: Context, uriStr: String, reqCode: Int): PendingIntent {
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(uriStr)).apply {
            setPackage(context.packageName)
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        return PendingIntent.getActivity(
            context,
            reqCode,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    private fun botNotificationId(botId: String): Int = 10000 + abs(botId.hashCode() % 10000)
    private fun botDoneNotificationId(botId: String): Int = 20000 + abs(botId.hashCode() % 10000)
    private fun takeoverNotificationId(id: String): Int = 30000 + abs(id.hashCode() % 10000)
    private fun approvalNotificationId(id: String): Int = 40000 + abs(id.hashCode() % 10000)
}

class BotActivityService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val names = intent?.getStringArrayListExtra(EXTRA_NAMES).orEmpty()
        val notification = NotificationHelper.buildForegroundNotification(this, names)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(9001, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            startForeground(9001, notification)
        }
        return START_NOT_STICKY
    }

    companion object {
        private const val EXTRA_NAMES = "bot_names"

        fun start(context: Context, botNames: List<String>) {
            runCatching {
                val intent = Intent(context, BotActivityService::class.java).apply {
                    putStringArrayListExtra(EXTRA_NAMES, ArrayList(botNames))
                }
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(intent)
                } else {
                    context.startService(intent)
                }
            }
        }

        fun stop(context: Context) {
            runCatching {
                context.stopService(Intent(context, BotActivityService::class.java))
            }
        }
    }
}
