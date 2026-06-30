namespace AR.Agent.Core.Geometry;

/// <summary>
/// Maps pointer positions on the agent's video panel to normalized (u,v) over the
/// source video frame. The panel shows the frame with uniform ("letterbox") fit,
/// so a naive control-relative mapping is wrong whenever the control and the video
/// have different aspect ratios — and a wrong (u,v) raycasts to the wrong place,
/// anchoring the annotation off-target. This accounts for the letterbox bars.
/// </summary>
public static class VideoCoords
{
    /// <summary>
    /// Converts a pointer position (in the control's pixel space) to normalized
    /// (u,v) in [0,1] over the source video. Returns null if the pointer lands in
    /// the letterbox/pillarbox bars (outside the displayed frame).
    /// </summary>
    public static (double U, double V)? PointerToNormalized(
        double pointerX, double pointerY,
        double controlWidth, double controlHeight,
        int sourceWidth, int sourceHeight)
    {
        if (controlWidth <= 0 || controlHeight <= 0 || sourceWidth <= 0 || sourceHeight <= 0)
            return null;

        // Uniform fit: scale the source to fit inside the control, preserving aspect.
        double scale = Math.Min(controlWidth / sourceWidth, controlHeight / sourceHeight);
        double displayedWidth = sourceWidth * scale;
        double displayedHeight = sourceHeight * scale;

        // The displayed frame is centred; bars fill the remaining space.
        double offsetX = (controlWidth - displayedWidth) / 2.0;
        double offsetY = (controlHeight - displayedHeight) / 2.0;

        double localX = pointerX - offsetX;
        double localY = pointerY - offsetY;
        if (localX < 0 || localY < 0 || localX > displayedWidth || localY > displayedHeight)
            return null; // clicked in the letterbox area

        return (localX / displayedWidth, localY / displayedHeight);
    }
}
