using AR.Agent.Core.Geometry;
using Xunit;

namespace AR.Agent.Core.Tests;

public class VideoCoordsTests
{
    [Fact]
    public void ExactFit_MapsDirectly()
    {
        // Control and source share aspect ratio: no bars.
        var n = VideoCoords.PointerToNormalized(640, 360, 1280, 720, 1280, 720);
        Assert.NotNull(n);
        Assert.Equal(0.5, n!.Value.U, 3);
        Assert.Equal(0.5, n.Value.V, 3);
    }

    [Fact]
    public void WideControl_Pillarboxes_CentreIsHalf()
    {
        // 16:9 source in a very wide control -> pillarbox bars left/right.
        var n = VideoCoords.PointerToNormalized(1000, 360, 2000, 720, 1280, 720);
        Assert.NotNull(n);
        Assert.Equal(0.5, n!.Value.U, 3);
        Assert.Equal(0.5, n.Value.V, 3);
    }

    [Fact]
    public void WideControl_ClickInLeftBar_ReturnsNull()
    {
        // Displayed width = 720*(1280/720)=1280, centred in 2000 -> bars of 360px.
        // x=100 is inside the left bar.
        var n = VideoCoords.PointerToNormalized(100, 360, 2000, 720, 1280, 720);
        Assert.Null(n);
    }

    [Fact]
    public void TallControl_Letterboxes_TopEdgeMapsToZero()
    {
        // 16:9 source in a tall control -> letterbox bars top/bottom.
        // Displayed height = 1280*(9/16)=720, centred in 1080 -> bars of 180px.
        var top = VideoCoords.PointerToNormalized(640, 180, 1280, 1080, 1280, 720);
        Assert.NotNull(top);
        Assert.Equal(0.0, top!.Value.V, 3);
        Assert.Equal(0.5, top.Value.U, 3);
    }

    [Fact]
    public void DegenerateInputs_ReturnNull()
    {
        Assert.Null(VideoCoords.PointerToNormalized(0, 0, 0, 100, 100, 100));
        Assert.Null(VideoCoords.PointerToNormalized(0, 0, 100, 100, 0, 100));
    }
}
