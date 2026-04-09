Add-Type -AssemblyName System.Drawing

$size = 256
$bmp = New-Object System.Drawing.Bitmap($size, $size)
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
$g.Clear([System.Drawing.Color]::Transparent)

$rect = New-Object System.Drawing.Rectangle(0, 0, ($size - 1), ($size - 1))
$path = New-Object System.Drawing.Drawing2D.GraphicsPath
$radius = 56
$diam = $radius * 2
$path.AddArc(8, 8, $diam, $diam, 180, 90)
$path.AddArc(($size - $diam - 8), 8, $diam, $diam, 270, 90)
$path.AddArc(($size - $diam - 8), ($size - $diam - 8), $diam, $diam, 0, 90)
$path.AddArc(8, ($size - $diam - 8), $diam, $diam, 90, 90)
$path.CloseFigure()

$bgBrush = New-Object System.Drawing.Drawing2D.LinearGradientBrush($rect, [System.Drawing.Color]::FromArgb(255,16,78,166), [System.Drawing.Color]::FromArgb(255,2,144,196), [System.Drawing.Drawing2D.LinearGradientMode]::ForwardDiagonal)
$g.FillPath($bgBrush, $path)

$ringPen = New-Object System.Drawing.Pen([System.Drawing.Color]::FromArgb(210,255,255,255), 7)
$g.DrawPath($ringPen, $path)

# database cylinder
$dbFill = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(242, 255, 255, 255))
$dbBorder = New-Object System.Drawing.Pen([System.Drawing.Color]::FromArgb(255, 227, 241, 255), 3)
$dbX = 92; $dbY = 122; $dbW = 72; $dbH = 80; $ellipseH = 20
$g.FillEllipse($dbFill, $dbX, $dbY, $dbW, $ellipseH)
$g.FillRectangle($dbFill, $dbX, ($dbY + [int]($ellipseH/2)), $dbW, ($dbH - $ellipseH))
$g.FillEllipse($dbFill, $dbX, ($dbY + $dbH - $ellipseH), $dbW, $ellipseH)
$g.DrawEllipse($dbBorder, $dbX, $dbY, $dbW, $ellipseH)
$g.DrawLine($dbBorder, $dbX, ($dbY + [int]($ellipseH/2)), $dbX, ($dbY + $dbH - [int]($ellipseH/2)))
$g.DrawLine($dbBorder, ($dbX + $dbW), ($dbY + [int]($ellipseH/2)), ($dbX + $dbW), ($dbY + $dbH - [int]($ellipseH/2)))
$g.DrawEllipse($dbBorder, $dbX, ($dbY + $dbH - $ellipseH), $dbW, $ellipseH)

# sync arrows
$arrowPen = New-Object System.Drawing.Pen([System.Drawing.Color]::FromArgb(255, 255, 255, 255), 12)
$arrowPen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
$arrowPen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
$g.DrawArc($arrowPen, 52, 58, 152, 152, 214, 118)
$g.DrawArc($arrowPen, 52, 58, 152, 152, 34, 118)

$headBrush = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::White)
$pts1 = [System.Drawing.Point[]]@(
  (New-Object System.Drawing.Point(192,86)),
  (New-Object System.Drawing.Point(220,92)),
  (New-Object System.Drawing.Point(204,114))
)
$pts2 = [System.Drawing.Point[]]@(
  (New-Object System.Drawing.Point(60,170)),
  (New-Object System.Drawing.Point(32,164)),
  (New-Object System.Drawing.Point(48,142))
)
$g.FillPolygon($headBrush, $pts1)
$g.FillPolygon($headBrush, $pts2)

$outDir = Join-Path (Get-Location) 'packaging'
if (-not (Test-Path $outDir)) { New-Item -ItemType Directory -Path $outDir | Out-Null }
$icoPath = Join-Path $outDir 'usbsync.ico'

$hIcon = $bmp.GetHicon()
$icon = [System.Drawing.Icon]::FromHandle($hIcon)
$fs = [System.IO.File]::Open($icoPath, [System.IO.FileMode]::Create)
$icon.Save($fs)
$fs.Close()

[System.Runtime.InteropServices.Marshal]::Release($hIcon) | Out-Null
$icon.Dispose()
$bgBrush.Dispose(); $ringPen.Dispose(); $dbFill.Dispose(); $dbBorder.Dispose(); $arrowPen.Dispose(); $headBrush.Dispose(); $path.Dispose(); $g.Dispose(); $bmp.Dispose()

Write-Output "icon generated: $icoPath"
