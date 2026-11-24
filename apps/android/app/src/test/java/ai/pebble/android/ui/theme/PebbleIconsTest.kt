package ai.pebble.android.ui.theme

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PebbleIconsTest {
    @Test
    fun allMaterialSymbolsDrawablesAreNonZeroAndDistinct() {
        val all = listOf(
            PebbleIcons.Add,
            PebbleIcons.ArrowBack,
            PebbleIcons.ArrowBackIosNew,
            PebbleIcons.ArrowCircleDown,
            PebbleIcons.ArrowDownward,
            PebbleIcons.ArrowDropDown,
            PebbleIcons.ArrowOutward,
            PebbleIcons.ArrowUpward,
            PebbleIcons.Chat,
            PebbleIcons.Check,
            PebbleIcons.CheckBox,
            PebbleIcons.CheckBoxOutlineBlank,
            PebbleIcons.CheckCircle,
            PebbleIcons.ChevronRight,
            PebbleIcons.Close,
            PebbleIcons.Code,
            PebbleIcons.Computer,
            PebbleIcons.ContentPaste,
            PebbleIcons.DeleteOutline,
            PebbleIcons.Description,
            PebbleIcons.DesktopWindows,
            PebbleIcons.Devices,
            PebbleIcons.ExpandLess,
            PebbleIcons.ExpandMore,
            PebbleIcons.FlashlightOff,
            PebbleIcons.FlashlightOn,
            PebbleIcons.Folder,
            PebbleIcons.FolderShared,
            PebbleIcons.FolderZip,
            PebbleIcons.FormatListBulleted,
            PebbleIcons.FormatQuote,
            PebbleIcons.Forum,
            PebbleIcons.Hub,
            PebbleIcons.Image,
            PebbleIcons.IosShare,
            PebbleIcons.Keyboard,
            PebbleIcons.KeyboardArrowDown,
            PebbleIcons.KeyboardArrowRight,
            PebbleIcons.List,
            PebbleIcons.MarkEmailRead,
            PebbleIcons.MarkEmailUnread,
            PebbleIcons.Memory,
            PebbleIcons.MoreVert,
            PebbleIcons.OpenInFull,
            PebbleIcons.PanTool,
            PebbleIcons.Pause,
            PebbleIcons.PhotoLibrary,
            PebbleIcons.PlayArrow,
            PebbleIcons.Psychology,
            PebbleIcons.PushPin,
            PebbleIcons.QrCodeScanner,
            PebbleIcons.Refresh,
            PebbleIcons.Reply,
            PebbleIcons.Search,
            PebbleIcons.Settings,
            PebbleIcons.Star,
            PebbleIcons.Stop,
            PebbleIcons.SwapVert,
            PebbleIcons.SyncProblem,
            PebbleIcons.Tune,
            PebbleIcons.Visibility,
            PebbleIcons.VisibilityOff,
            PebbleIcons.VpnKey,
            PebbleIcons.Warning,
        )
        assertEquals(64, all.size)
        assertTrue(all.all { it != 0 })
        assertEquals(all.size, all.toSet().size)
    }
}
