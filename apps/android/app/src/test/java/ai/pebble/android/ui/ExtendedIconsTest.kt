package ai.pebble.android.ui

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Chat
import androidx.compose.material.icons.automirrored.rounded.FormatListBulleted
import androidx.compose.material.icons.automirrored.rounded.Reply
import androidx.compose.material.icons.rounded.ArrowBackIosNew
import androidx.compose.material.icons.rounded.ArrowCircleDown
import androidx.compose.material.icons.rounded.ArrowDownward
import androidx.compose.material.icons.rounded.ArrowOutward
import androidx.compose.material.icons.rounded.ArrowUpward
import androidx.compose.material.icons.rounded.CheckBox
import androidx.compose.material.icons.rounded.CheckBoxOutlineBlank
import androidx.compose.material.icons.rounded.ChevronRight
import androidx.compose.material.icons.rounded.Code
import androidx.compose.material.icons.rounded.Computer
import androidx.compose.material.icons.rounded.ContentPaste
import androidx.compose.material.icons.rounded.DeleteOutline
import androidx.compose.material.icons.rounded.Description
import androidx.compose.material.icons.rounded.DesktopWindows
import androidx.compose.material.icons.rounded.Devices
import androidx.compose.material.icons.rounded.ExpandLess
import androidx.compose.material.icons.rounded.ExpandMore
import androidx.compose.material.icons.rounded.FlashlightOff
import androidx.compose.material.icons.rounded.FlashlightOn
import androidx.compose.material.icons.rounded.Folder
import androidx.compose.material.icons.rounded.FolderShared
import androidx.compose.material.icons.rounded.FolderZip
import androidx.compose.material.icons.rounded.FormatQuote
import androidx.compose.material.icons.rounded.Forum
import androidx.compose.material.icons.rounded.Hub
import androidx.compose.material.icons.rounded.Image
import androidx.compose.material.icons.rounded.IosShare
import androidx.compose.material.icons.rounded.Keyboard
import androidx.compose.material.icons.rounded.MarkEmailRead
import androidx.compose.material.icons.rounded.MarkEmailUnread
import androidx.compose.material.icons.rounded.Memory
import androidx.compose.material.icons.rounded.OpenInFull
import androidx.compose.material.icons.rounded.PanTool
import androidx.compose.material.icons.rounded.Pause
import androidx.compose.material.icons.rounded.PhotoLibrary
import androidx.compose.material.icons.rounded.Psychology
import androidx.compose.material.icons.rounded.PushPin
import androidx.compose.material.icons.rounded.QrCodeScanner
import androidx.compose.material.icons.rounded.Stop
import androidx.compose.material.icons.rounded.SwapVert
import androidx.compose.material.icons.rounded.SyncProblem
import androidx.compose.material.icons.rounded.Tune
import androidx.compose.material.icons.rounded.Visibility
import androidx.compose.material.icons.rounded.VisibilityOff
import androidx.compose.material.icons.rounded.VpnKey
import org.junit.Assert.assertTrue
import org.junit.Test

class ExtendedIconsTest {
    @Test
    fun allExtractedIconsParseValidPaths() {
        val icons = listOf(
            Icons.Rounded.ArrowBackIosNew,
            Icons.Rounded.ArrowCircleDown,
            Icons.Rounded.ArrowDownward,
            Icons.Rounded.ArrowOutward,
            Icons.Rounded.ArrowUpward,
            Icons.Rounded.CheckBox,
            Icons.Rounded.CheckBoxOutlineBlank,
            Icons.Rounded.ChevronRight,
            Icons.Rounded.Code,
            Icons.Rounded.Computer,
            Icons.Rounded.ContentPaste,
            Icons.Rounded.DeleteOutline,
            Icons.Rounded.Description,
            Icons.Rounded.DesktopWindows,
            Icons.Rounded.Devices,
            Icons.Rounded.ExpandLess,
            Icons.Rounded.ExpandMore,
            Icons.Rounded.FlashlightOff,
            Icons.Rounded.FlashlightOn,
            Icons.Rounded.Folder,
            Icons.Rounded.FolderShared,
            Icons.Rounded.FolderZip,
            Icons.Rounded.FormatQuote,
            Icons.Rounded.Forum,
            Icons.Rounded.Hub,
            Icons.Rounded.Image,
            Icons.Rounded.IosShare,
            Icons.Rounded.Keyboard,
            Icons.Rounded.MarkEmailRead,
            Icons.Rounded.MarkEmailUnread,
            Icons.Rounded.Memory,
            Icons.Rounded.OpenInFull,
            Icons.Rounded.PanTool,
            Icons.Rounded.Pause,
            Icons.Rounded.PhotoLibrary,
            Icons.Rounded.Psychology,
            Icons.Rounded.PushPin,
            Icons.Rounded.QrCodeScanner,
            Icons.Rounded.Stop,
            Icons.Rounded.SwapVert,
            Icons.Rounded.SyncProblem,
            Icons.Rounded.Tune,
            Icons.Rounded.Visibility,
            Icons.Rounded.VisibilityOff,
            Icons.Rounded.VpnKey,
            Icons.AutoMirrored.Rounded.Chat,
            Icons.AutoMirrored.Rounded.FormatListBulleted,
            Icons.AutoMirrored.Rounded.Reply,
        )
        for (icon in icons) {
            assertTrue("Icon ${icon.name} should have non-empty root children", icon.root.size > 0)
        }
    }
}
