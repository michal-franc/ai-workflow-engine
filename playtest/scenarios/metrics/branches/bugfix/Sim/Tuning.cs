namespace Siege.Sim;

/// <summary>Every balance knob in one place.</summary>
public static class Tuning
{
    public const float OpeningPrep = 20;   // stage 1: the defenders' set-up before the clock starts
    public const float PrepTime = 30;      // stages 2-3: the objective stays locked this long
    public const float RunSpeed = 5.5f;    // metres per second
    public const float CallNoise = 0.55f;  // AI call scores blurred by this
    public const float BanNoise = 0.3f;    // ban scores blurred by this
}
